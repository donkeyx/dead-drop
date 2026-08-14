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
	"path/filepath"
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
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /s/{id}", s.handleReveal)
	mux.HandleFunc("GET /about", s.handleAbout)
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFiles(http.Dir(s.cfg.StaticDir))))
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /startupz", s.handleStartupz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("POST /api/v1/secrets", s.handleCreate)
	mux.HandleFunc("GET /api/v1/secrets/{id}", s.handleGet)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No CORS by design.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.writeShell(w)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	s.writeShell(w)
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	s.writePage(w, aboutPage)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if s.cfg.StaticDir == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "favicon.ico"))
}

func staticFiles(root http.FileSystem) http.Handler {
	fs := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(&staticCacheWriter{ResponseWriter: w}, r)
	})
}

// staticCacheWriter caches successful assets briefly and never caches 404s.
type staticCacheWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *staticCacheWriter) WriteHeader(code int) {
	if !w.wrote {
		if code == http.StatusOK {
			w.Header().Set("Cache-Control", "public, max-age=300")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *staticCacheWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (s *Server) writeShell(w http.ResponseWriter) {
	s.writePage(w, uiShell)
}

func (s *Server) writePage(w http.ResponseWriter, page string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

const uiShell = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>dead-drop</title>
  <link rel="icon" href="/static/favicon.ico?v=1" sizes="any">
  <link rel="icon" type="image/png" href="/static/favicon.png?v=1" sizes="32x32">
  <link rel="apple-touch-icon" href="/static/apple-touch-icon.png?v=1">
  <link rel="stylesheet" href="/static/skin.css?v=1">
</head>
<body>
  <div class="wrap">
    <header class="brand">
      <img src="/static/mark.jpg?v=1" width="88" height="88" alt="">
      <div>
        <h1>dead-drop</h1>
        <p class="tag">Client-side encrypted. The server only ever sees ciphertext.</p>
        <span class="org">donkeyx</span>
      </div>
    </header>
    <main>
      <section class="panel" id="create-panel">
        <h2>Leave a drop</h2>
        <form id="create-form">
          <label for="secret">Secret or small file</label>
          <div class="input-with-action secret-input">
            <textarea id="secret" class="privacy-mode" name="secret" autocomplete="off" maxlength="16777216" placeholder="Type a secret message..."></textarea>
            <button class="visibility-toggle" type="button" data-toggle-visibility="secret" aria-label="Show secret" title="Show secret">◉</button>
          </div>
          <div class="file-pick">
            <label for="file">Or attach a small file</label>
            <input id="file" type="file" accept="*/*">
          </div>
          <p class="muted"><span class="lock-mark" aria-hidden="true">◆</span> Encrypted in this browser. Never uploaded as plaintext. Maximum 16 MiB.</p>
          <label for="passphrase">Optional passphrase</label>
          <div class="input-with-action">
            <input id="passphrase" name="passphrase" type="password" autocomplete="off">
            <button class="visibility-toggle" type="button" data-toggle-visibility="passphrase" aria-label="Show passphrase" title="Show passphrase">◉</button>
          </div>
          <label><input id="burn" type="checkbox" checked> Burn after first download</label>
          <button class="primary-action" type="submit">Create encrypted link</button>
        </form>
        <output id="create-result" aria-live="polite"></output>
      </section>
      <section class="panel" id="reveal-panel" hidden>
        <h2>Open drop</h2>
        <p class="warning">The first download consumes burn-after-read drops, even if decryption fails.</p>
        <label for="reveal-passphrase">Passphrase, if required</label>
        <div class="input-with-action">
          <input id="reveal-passphrase" type="password" autocomplete="off">
          <button class="visibility-toggle" type="button" data-toggle-visibility="reveal-passphrase" aria-label="Show passphrase" title="Show passphrase">◉</button>
        </div>
        <button id="open-drop" type="button">Open encrypted drop</button>
        <output id="reveal-result" aria-live="polite"></output>
      </section>
    </main>
    <p class="foot">A <b>donkeyx</b> drop. Encrypt first. Leave nothing the operator can read. <a href="/about">How it works</a></p>
  </div>
  <script src="/static/wasm_exec.js"></script>
  <script src="/static/deaddrop.js"></script>
  <script src="/static/ui.js"></script>
</body>
</html>`

const aboutPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>How dead-drop works</title>
  <link rel="icon" href="/static/favicon.ico?v=1" sizes="any">
  <link rel="stylesheet" href="/static/skin.css?v=1">
</head>
<body>
  <div class="wrap info-page">
    <header class="brand">
      <img src="/static/mark.jpg?v=1" width="88" height="88" alt="">
      <div>
        <h1>dead-drop</h1>
        <p class="tag">A transparent envelope for secrets.</p>
        <span class="org">donkeyx</span>
      </div>
    </header>
    <main class="panel">
      <h2>How it works</h2>
      <p>Your browser encrypts the text or file before it leaves your device. The server stores and returns only the encrypted blob.</p>
      <p>The decryption key stays in the URL fragment after the <code>#</code>. Browsers do not send URL fragments in HTTP requests, so the server never receives that key.</p>
      <h2>What to trust</h2>
      <ul>
        <li>The source code is public at <a href="https://github.com/donkeyx/dead-drop">github.com/donkeyx/dead-drop</a>.</li>
        <li>Burn-after-read is atomic: one successful download consumes a drop.</li>
        <li>Client-side encryption is not a promise that the hosting server is harmless. Verify the code or run your own instance if you do not trust this one.</li>
      </ul>
      <p class="warning">Do not use this service for anything where you cannot accept the risk of a compromised browser, server, or deployment.</p>
      <p class="foot"><a href="/">Back to dead-drop</a></p>
    </main>
  </div>
</body>
</html>`

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleStartupz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("started\n"))
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
