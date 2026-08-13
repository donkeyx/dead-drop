package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/donkeyx/dead-drop/internal/config"
	"github.com/donkeyx/dead-drop/server"
	"github.com/donkeyx/dead-drop/store"
)

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "", "listen address (default DEADDROP_ADDR or :8080)")
	data := fs.String("data", "", "data directory (default DEADDROP_DATA or ./data)")
	static := fs.String("static", "", "static assets directory (default DEADDROP_STATIC or ./web/static)")
	storeKind := fs.String("store", "", "sqlite|fs (default DEADDROP_STORE or sqlite)")
	databaseURL := fs.String("database-url", "", "PostgreSQL URL (default DEADDROP_DATABASE_URL when -store=postgres)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: config: %v\n", err)
		return exitUsage
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if *data != "" {
		cfg.DataDir = *data
	}
	if *static != "" {
		cfg.StaticDir = *static
	}
	if *storeKind != "" {
		cfg.Store = *storeKind
	}
	if *databaseURL != "" {
		cfg.DatabaseURL = *databaseURL
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var st store.Store
	switch cfg.Store {
	case "fs":
		st, err = store.OpenFS(cfg.DataDir)
	case "postgres":
		st, err = store.OpenPostgres(cfg.DatabaseURL)
	default:
		st, err = store.OpenSQLite(cfg.DataDir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: open store: %v\n", err)
		return exitUsage
	}
	defer st.Close()

	srv := server.New(cfg, st, log)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.Addr, "store", cfg.Store, "data", cfg.DataDir)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Info("shutdown complete")
	return exitOK
}
