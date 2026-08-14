// Package config holds server defaults (env DEADDROP_*).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is process configuration for dead-drop serve.
type Config struct {
	Addr           string
	DataDir        string
	StaticDir      string
	Store          string // sqlite | fs
	DatabaseURL    string
	MaxBytes       int64
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	MinTTL         time.Duration
	TrustProxy     bool
	TrustedProxies []string // CIDRs
	LogIDsFull     bool

	// Rate limits (lenient v1 defaults; PR8 may tune).
	CreatePerIP  int
	CreateWindow time.Duration
	GetPerIP     int
	GetWindow    time.Duration

	// Turnstile is optional. Empty secret keeps current behaviour (self-host / CI).
	TurnstileSiteKey   string
	TurnstileSecret    string
	TurnstileVerifyURL string
	SmokeBypass        string
}

// LoadFromEnv reads DEADDROP_* with sensible defaults from DESIGN.md.
func LoadFromEnv() (Config, error) {
	c := Config{
		Addr:               env("DEADDROP_ADDR", ":8080"),
		DataDir:            env("DEADDROP_DATA", "./data"),
		StaticDir:          env("DEADDROP_STATIC", "./web/static"),
		Store:              strings.ToLower(env("DEADDROP_STORE", "sqlite")),
		DatabaseURL:        env("DEADDROP_DATABASE_URL", ""),
		MaxBytes:           envInt64("DEADDROP_MAX_BYTES", 16<<20),
		DefaultTTL:         envDuration("DEADDROP_DEFAULT_TTL", 24*time.Hour),
		MaxTTL:             envDuration("DEADDROP_MAX_TTL", 7*24*time.Hour),
		MinTTL:             envDuration("DEADDROP_MIN_TTL", 5*time.Minute),
		TrustProxy:         envBool("DEADDROP_TRUST_PROXY", false),
		LogIDsFull:         env("DEADDROP_LOG_IDS", "truncate") == "full",
		CreatePerIP:        20,
		CreateWindow:       15 * time.Minute,
		GetPerIP:           60,
		GetWindow:          15 * time.Minute,
		TurnstileSiteKey:   env("DEADDROP_TURNSTILE_SITE_KEY", ""),
		TurnstileSecret:    env("DEADDROP_TURNSTILE_SECRET", ""),
		TurnstileVerifyURL: env("DEADDROP_TURNSTILE_VERIFY_URL", ""),
		SmokeBypass:        env("DEADDROP_SMOKE_BYPASS", ""),
	}
	if raw := env("DEADDROP_TRUSTED_PROXIES", ""); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				c.TrustedProxies = append(c.TrustedProxies, p)
			}
		}
	}
	if c.TrustProxy && len(c.TrustedProxies) == 0 {
		return c, fmt.Errorf("DEADDROP_TRUST_PROXY=true requires DEADDROP_TRUSTED_PROXIES")
	}
	if c.Store != "sqlite" && c.Store != "fs" && c.Store != "postgres" {
		return c, fmt.Errorf("DEADDROP_STORE must be sqlite, fs, or postgres")
	}
	if c.Store == "postgres" && c.DatabaseURL == "" {
		return c, fmt.Errorf("DEADDROP_STORE=postgres requires DEADDROP_DATABASE_URL")
	}
	if c.DefaultTTL > c.MaxTTL || c.DefaultTTL < c.MinTTL {
		return c, fmt.Errorf("invalid TTL defaults")
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt64(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
