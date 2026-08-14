package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/donkeyx/dead-drop/internal/config"
	"github.com/donkeyx/dead-drop/store"
)

func cmdExpire(args []string) int {
	fs := flag.NewFlagSet("expire", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	data := fs.String("data", "", "data directory (default DEADDROP_DATA or ./data)")
	storeKind := fs.String("store", "", "sqlite|fs|postgres (default DEADDROP_STORE or sqlite)")
	databaseURL := fs.String("database-url", "", "PostgreSQL URL (default DEADDROP_DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "expire: config: %v\n", err)
		return exitUsage
	}
	if *data != "" {
		cfg.DataDir = *data
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
		fmt.Fprintf(os.Stderr, "expire: open store: %v\n", err)
		return exitUsage
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := st.DeleteExpired(ctx, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "expire: %v\n", err)
		return exitUsage
	}
	log.Info("expired rows removed", "deleted", n, "store", cfg.Store)
	return exitOK
}
