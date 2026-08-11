// Package server implements the dead-drop HTTP API (no UI).
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/donkeyx/dead-drop/blob"
	"github.com/donkeyx/dead-drop/internal/config"
	"github.com/donkeyx/dead-drop/internal/ratelimit"
	"github.com/donkeyx/dead-drop/store"
)

// Server is the HTTP API.
type Server struct {
	cfg    config.Config
	st     store.Store
	log    *slog.Logger
	create *ratelimit.Limiter
	get    *ratelimit.Limiter
}

// New wires handlers over an open store.
func New(cfg config.Config, st store.Store, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		cfg:    cfg,
		st:     st,
		log:    log,
		create: ratelimit.New(cfg.CreatePerIP, cfg.CreateWindow),
		get:    ratelimit.New(cfg.GetPerIP, cfg.GetWindow),
	}
}

// Handler returns the root mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("POST /api/v1/secrets", s.handleCreate)
	mux.HandleFunc("GET /api/v1/secrets/{id}", s.handleGet)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No CORS by design.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Store is open if we have a process lock; Count is a cheap touch.
	if _, err := s.st.Count(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "storage_full", "storage not ready")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

type createJSON struct {
	Blob          string `json:"blob"`
	TTLSeconds    *int64 `json:"ttl_seconds"`
	BurnAfterRead *bool  `json:"burn_after_read"`
}

type createResp struct {
	ID            string    `json:"id"`
	ExpiresAt     time.Time `json:"expires_at"`
	BurnAfterRead bool      `json:"burn_after_read"`
	Size          int64     `json:"size"`
	Path          string    `json:"path"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.cfg)
	if ok, retry := s.create.Allow(ip, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "rate_limit", "too many creates")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBytes+1024)

	var (
		blobBytes []byte
		ttl       = s.cfg.DefaultTTL
		burn      = true
	)

	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "application/json"):
		var body createJSON
		dec := json.NewDecoder(io.LimitReader(r.Body, s.cfg.MaxBytes+2048))
		if err := dec.Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid json")
			return
		}
		b, err := base64.RawURLEncoding.DecodeString(body.Blob)
		if err != nil {
			b, err = base64.URLEncoding.DecodeString(body.Blob)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_blob", "invalid blob encoding")
			return
		}
		blobBytes = b
		if body.TTLSeconds != nil {
			if *body.TTLSeconds < 0 {
				writeErr(w, http.StatusBadRequest, "bad_ttl", "ttl_seconds must be >= 0")
				return
			}
			ttl = time.Duration(*body.TTLSeconds) * time.Second
		}
		if body.BurnAfterRead != nil {
			burn = *body.BurnAfterRead
		}
	default:
		// application/octet-stream (preferred)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			// MaxBytesReader returns error when too large
			if strings.Contains(err.Error(), "request body too large") {
				writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "body exceeds max size")
				return
			}
			writeErr(w, http.StatusBadRequest, "bad_request", "read body failed")
			return
		}
		blobBytes = b
		if h := r.Header.Get("X-Seal-TTL"); h != "" {
			d, err := time.ParseDuration(h)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "bad_ttl", "X-Seal-TTL must be a Go duration (e.g. 24h)")
				return
			}
			ttl = d
		}
		if h := r.Header.Get("X-Seal-Burn"); h != "" {
			switch h {
			case "1", "true", "TRUE", "yes":
				burn = true
			case "0", "false", "FALSE", "no":
				burn = false
			default:
				writeErr(w, http.StatusBadRequest, "bad_request", "X-Seal-Burn must be 0 or 1")
				return
			}
		}
	}

	if int64(len(blobBytes)) > s.cfg.MaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "body exceeds max size")
		return
	}
	if err := validateSEALFraming(blobBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_blob", err.Error())
		return
	}
	if ttl < s.cfg.MinTTL || ttl > s.cfg.MaxTTL {
		writeErr(w, http.StatusBadRequest, "bad_ttl", "ttl out of allowed range")
		return
	}

	now := time.Now().UTC()
	meta := store.Meta{
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
		BurnAfterRead: burn,
		Size:          int64(len(blobBytes)),
		FormatVersion: blob.VersionV1,
	}

	var id string
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		id, err = mintID()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "bad_request", "id generation failed")
			return
		}
		meta.ID = id
		err = s.st.Create(r.Context(), meta, blobBytes)
		if err == nil {
			break
		}
		if errors.Is(err, store.ErrExists) {
			continue
		}
		s.log.Error("create failed", "err", err)
		writeErr(w, http.StatusServiceUnavailable, "storage_full", "storage error")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bad_request", "could not allocate id")
		return
	}

	s.log.Info("secret created", "id", truncateID(id, s.cfg.LogIDsFull), "size", len(blobBytes), "burn", burn)
	writeJSON(w, http.StatusCreated, createResp{
		ID:            id,
		ExpiresAt:     meta.ExpiresAt,
		BurnAfterRead: burn,
		Size:          meta.Size,
		Path:          "/s/" + id,
	})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, s.cfg)
	if ok, retry := s.get.Allow(ip, time.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "rate_limit", "too many fetches")
		return
	}

	id := r.PathValue("id")
	if id == "" || len(id) > 64 {
		writeErr(w, http.StatusNotFound, "not_found", "not found")
		return
	}

	// Normative: Take only — never Get-then-Delete.
	rec, err := s.st.Take(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		s.log.Error("take failed", "id", truncateID(id, s.cfg.LogIDsFull), "err", err)
		writeErr(w, http.StatusServiceUnavailable, "storage_full", "storage error")
		return
	}

	w.Header().Set("X-Seal-Burn-After-Read", strconv.FormatBool(rec.Meta.BurnAfterRead))
	w.Header().Set("X-Seal-Expires-At", rec.Meta.ExpiresAt.UTC().Format(time.RFC3339))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if r.URL.Query().Get("alt") == "json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"blob":            base64.RawURLEncoding.EncodeToString(rec.Blob),
			"burn_after_read": rec.Meta.BurnAfterRead,
			"expires_at":      rec.Meta.ExpiresAt.UTC().Format(time.RFC3339),
			"size":            len(rec.Blob),
		})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(rec.Blob)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rec.Blob)
}

type errBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, errBody{Error: errCode, Message: msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func mintID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func truncateID(id string, full bool) string {
	if full || len(id) < 6 {
		return id
	}
	return id[:2] + "…" + id[len(id)-2:]
}

func validateSEALFraming(b []byte) error {
	// Minimum package size from design (no passphrase): 48 + empty envelope still > 8
	if len(b) < 8+24+16 {
		return errors.New("package too small")
	}
	if string(b[0:4]) != blob.Magic {
		return errors.New("bad magic")
	}
	if b[4] != blob.VersionV1 {
		return errors.New("unsupported version")
	}
	return nil
}

func clientIP(r *http.Request, cfg config.Config) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !cfg.TrustProxy {
		return host
	}
	// Trusted proxy: only if peer in allowlist (simplified: check first CIDR match on peer)
	peer := net.ParseIP(host)
	if peer == nil || !ipInAnyCIDR(peer, cfg.TrustedProxies) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return host
}

func ipInAnyCIDR(ip net.IP, cidrs []string) bool {
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			if c == ip.String() {
				return true
			}
			continue
		}
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
