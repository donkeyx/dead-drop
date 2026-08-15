package observe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteNeverIncludesID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/abc123secretid", nil)
	if got := Route(req); got != "/api/v1/secrets/{id}" {
		t.Fatalf("got %q", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/s/abc123secretid", nil)
	if got := Route(req); got != "/s/{id}" {
		t.Fatalf("got %q", got)
	}
}

func TestMetricsHandlerAfterStart(t *testing.T) {
	if err := Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	Created(context.Background())
	Fetched(context.Background(), "ok")
	Fetched(context.Background(), "not_found")
	Burned(context.Background())

	rr := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	got := string(body)
	for _, want := range []string{
		"deaddrop_secrets_created_total",
		"deaddrop_secrets_fetched_total",
		"deaddrop_secrets_burned_total",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "abc123") {
		t.Fatal("metrics leaked an id-like value")
	}
}
