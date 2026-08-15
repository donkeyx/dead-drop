// Package observe is process metrics and optional Grafana Cloud OTLP export.
//
// Counters never carry secret ids, IPs, or ciphertext. /metrics is served
// only from the address in DEADDROP_METRICS_ADDR (empty = off), not the
// public HTTP mux. When OTEL_EXPORTER_OTLP_ENDPOINT is set, the same
// instruments and traces are pushed there (Grafana Cloud OTLP gateway).
package observe

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const scope = "github.com/donkeyx/dead-drop"

var (
	startOnce sync.Once
	startErr  error
	shutdowns []func(context.Context) error
	metricsH  http.Handler
	tracing   bool

	secretsCreated metric.Int64Counter
	secretsFetched metric.Int64Counter
	secretsBurned  metric.Int64Counter
	httpReqs       metric.Int64Counter
	httpDur        metric.Float64Histogram
)

// Start installs the Prometheus /metrics gatherer and, when
// OTEL_EXPORTER_OTLP_ENDPOINT is set, OTLP metric + trace exporters.
// Safe to call more than once.
func Start(ctx context.Context) error {
	startOnce.Do(func() {
		startErr = start(ctx)
	})
	return startErr
}

func start(ctx context.Context) error {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(semconv.ServiceName("dead-drop")),
	)
	if err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	promExp, err := otelprom.New(
		otelprom.WithRegisterer(reg),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return err
	}
	metricsH = promhttp.HandlerFor(reg, promhttp.HandlerOpts{DisableCompression: true})

	meterOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExp),
	}

	if endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); endpoint != "" {
		texp, err := otlptracehttp.New(ctx)
		if err != nil {
			return err
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(texp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		shutdowns = append(shutdowns, tp.Shutdown)
		tracing = true

		mexp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return err
		}
		meterOpts = append(meterOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp)))
	}

	mp := sdkmetric.NewMeterProvider(meterOpts...)
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	meter := mp.Meter(scope)
	if secretsCreated, err = meter.Int64Counter("deaddrop.secrets.created",
		metric.WithDescription("Secrets created")); err != nil {
		return err
	}
	if secretsFetched, err = meter.Int64Counter("deaddrop.secrets.fetched",
		metric.WithDescription("Secret Take results")); err != nil {
		return err
	}
	if secretsBurned, err = meter.Int64Counter("deaddrop.secrets.burned",
		metric.WithDescription("Burn-after-read Takes that consumed the drop")); err != nil {
		return err
	}
	if httpReqs, err = meter.Int64Counter("deaddrop.http.requests",
		metric.WithDescription("HTTP requests")); err != nil {
		return err
	}
	if httpDur, err = meter.Float64Histogram("deaddrop.http.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s")); err != nil {
		return err
	}
	return nil
}

// Shutdown flushes exporters. Safe if Start was never called.
func Shutdown(ctx context.Context) {
	for i := len(shutdowns) - 1; i >= 0; i-- {
		_ = shutdowns[i](ctx)
	}
}

// MetricsHandler serves Prometheus text. Nil until Start succeeds.
func MetricsHandler() http.Handler {
	if metricsH == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "metrics not started", http.StatusServiceUnavailable)
		})
	}
	return metricsH
}

// Tracing reports whether an OTLP trace exporter is running.
func Tracing() bool { return tracing }

// Created increments deaddrop_secrets_created_total.
func Created(ctx context.Context) {
	if secretsCreated != nil {
		secretsCreated.Add(ctx, 1)
	}
}

// Fetched increments deaddrop_secrets_fetched_total. result is ok or not_found.
func Fetched(ctx context.Context, result string) {
	if secretsFetched != nil {
		secretsFetched.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	}
}

// Burned increments deaddrop_secrets_burned_total.
func Burned(ctx context.Context) {
	if secretsBurned != nil {
		secretsBurned.Add(ctx, 1)
	}
}

// HTTP records request count and duration. Route labels are templates only.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := Route(r)
		ctx := r.Context()
		if httpReqs != nil {
			httpReqs.Add(ctx, 1, metric.WithAttributes(
				attribute.String("code", strconv.Itoa(sw.code)),
				attribute.String("route", route),
			))
		}
		if httpDur != nil {
			httpDur.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
				attribute.String("route", route),
			))
		}
	})
}

// Route is a low-cardinality path template. Never includes a secret id.
func Route(r *http.Request) string {
	path := r.URL.Path
	switch {
	case path == "/":
		return "/"
	case path == "/about":
		return "/about"
	case path == "/healthz":
		return "/healthz"
	case path == "/readyz":
		return "/readyz"
	case path == "/startupz":
		return "/startupz"
	case path == "/favicon.ico":
		return "/favicon.ico"
	case path == "/api/v1/secrets" && r.Method == http.MethodPost:
		return "/api/v1/secrets"
	case strings.HasPrefix(path, "/api/v1/secrets/"):
		return "/api/v1/secrets/{id}"
	case strings.HasPrefix(path, "/s/"):
		return "/s/{id}"
	case strings.HasPrefix(path, "/static/"):
		return "/static/*"
	default:
		return "other"
	}
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
