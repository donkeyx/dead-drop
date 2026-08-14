package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/donkeyx/dead-drop/blob"
	"github.com/donkeyx/dead-drop/internal/config"
	"github.com/donkeyx/dead-drop/store"
)

func testServer(t *testing.T) (*Server, store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Config{
		MaxBytes:     1 << 20,
		DefaultTTL:   time.Hour,
		MaxTTL:       24 * time.Hour,
		MinTTL:       time.Minute,
		CreatePerIP:  1000,
		CreateWindow: time.Hour,
		GetPerIP:     1000,
		GetWindow:    time.Hour,
	}
	return New(cfg, st, nil), st
}

func sealBlob(t *testing.T) []byte {
	t.Helper()
	mk, err := blob.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := blob.Seal([]byte("api secret"), mk, blob.SealOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestCreateAndGetOctetStream(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	pkg := sealBlob(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Seal-TTL", "1h")
	req.Header.Set("X-Seal-Burn", "0")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var cr createResp
	if err := json.Unmarshal(rr.Body.Bytes(), &cr); err != nil {
		t.Fatal(err)
	}
	if cr.ID == "" || cr.Path != "/s/"+cr.ID {
		t.Fatalf("%+v", cr)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get %d", rr.Code)
	}
	if !bytes.Equal(rr.Body.Bytes(), pkg) {
		t.Fatal("blob mismatch")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache header")
	}
	// multi-read still works
	req = httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("second get %d", rr.Code)
	}
}

func TestGetBurnParallel(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	pkg := sealBlob(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Seal-Burn", "1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatal(rr.Body.String())
	}
	var cr createResp
	_ = json.Unmarshal(rr.Body.Bytes(), &cr)

	const n = 20
	var ok atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("expected 1 success, got %d", ok.Load())
	}
	// burned
	req = httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestCreateJSONAndGetAltJSON(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	pkg := sealBlob(t)
	body, _ := json.Marshal(map[string]any{
		"blob":            blob.EncodeKeyB64URL(pkg),
		"ttl_seconds":     3600,
		"burn_after_read": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatal(rr.Body.String())
	}
	var cr createResp
	_ = json.Unmarshal(rr.Body.Bytes(), &cr)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID+"?alt=json", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["blob"] == nil {
		t.Fatal(got)
	}
	encoded, ok := got["blob"].(string)
	if !ok {
		t.Fatalf("blob type = %T", got["blob"])
	}
	decoded, err := blob.DecodeB64URL(encoded)
	if err != nil || !bytes.Equal(decoded, pkg) {
		t.Fatalf("alt=json blob did not round-trip")
	}
}

func TestBadBlob(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader([]byte("notaseal")))
	req.Header.Set("Content-Type", "application/octet-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d", rr.Code)
	}
}

func TestRateLimit(t *testing.T) {
	dir := t.TempDir()
	st, err := store.OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Config{
		MaxBytes: 1 << 20, DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, MinTTL: time.Minute,
		CreatePerIP: 2, CreateWindow: time.Hour,
		GetPerIP: 100, GetWindow: time.Hour,
	}
	srv := New(cfg, st, nil)
	h := srv.Handler()
	pkg := sealBlob(t)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.RemoteAddr = "1.2.3.4:1234"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("%d: %s", rr.Code, rr.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.RemoteAddr = "1.2.3.4:9999"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestHomeTurnstileCSP(t *testing.T) {
	srv, _ := testServer(t)
	srv.cfg.TurnstileSiteKey = "site-key-test"
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "challenges.cloudflare.com") {
		t.Fatalf("csp=%s", csp)
	}
	if !strings.Contains(rr.Body.String(), `data-turnstile-sitekey="site-key-test"`) {
		t.Fatal("missing site key on html")
	}
}

func TestHomeOmitsTurnstileForSmoke(t *testing.T) {
	srv, _ := testServer(t)
	srv.cfg.TurnstileSiteKey = "site-key-test"
	srv.cfg.SmokeBypass = "smoke-secret"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-dead-drop-smoke", "smoke-secret")
	srv.Handler().ServeHTTP(rr, req)
	if strings.Contains(rr.Body.String(), `data-turnstile-sitekey="site-key-test"`) {
		t.Fatal("smoke request should not receive the Turnstile site key")
	}
}

func TestCreateRequiresTurnstile(t *testing.T) {
	dir := t.TempDir()
	st, err := store.OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var sawToken string
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sawToken = r.Form.Get("response")
		ok := sawToken == "good-token"
		_ = json.NewEncoder(w).Encode(map[string]any{"success": ok})
	}))
	t.Cleanup(verify.Close)

	cfg := config.Config{
		MaxBytes: 1 << 20, DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, MinTTL: time.Minute,
		CreatePerIP: 100, CreateWindow: time.Hour, GetPerIP: 100, GetWindow: time.Hour,
		TurnstileSiteKey: "site", TurnstileSecret: "sec", TurnstileVerifyURL: verify.URL,
		SmokeBypass: "smoke-secret",
	}
	h := New(cfg, st, nil).Handler()
	pkg := sealBlob(t)

	post := func(headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
		req.Header.Set("Content-Type", "application/octet-stream")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	if rr := post(nil); rr.Code != http.StatusForbidden {
		t.Fatalf("no token: want 403 got %d %s", rr.Code, rr.Body.String())
	}
	if rr := post(map[string]string{"CF-Turnstile-Response": "bad-token"}); rr.Code != http.StatusForbidden {
		t.Fatalf("bad token: want 403 got %d %s", rr.Code, rr.Body.String())
	}
	if rr := post(map[string]string{"CF-Turnstile-Response": "good-token"}); rr.Code != http.StatusCreated {
		t.Fatalf("good token: want 201 got %d %s", rr.Code, rr.Body.String())
	}
	if sawToken != "good-token" {
		t.Fatalf("verify saw %q", sawToken)
	}
	if rr := post(map[string]string{"x-dead-drop-smoke": "smoke-secret"}); rr.Code != http.StatusCreated {
		t.Fatalf("smoke bypass: want 201 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
}

func TestNoCORS(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected CORS")
	}
}

func TestUIHeadersAndShell(t *testing.T) {
	srv, _ := testServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte("Client-side encrypted")) {
		t.Fatalf("home response: %d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("/static/skin.css?v=4")) {
		t.Fatal("UI shell does not load the cache-busted skin")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("/static/favicon.ico?v=1")) {
		t.Fatal("UI shell does not load the favicon")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("/static/favicon.png?v=1")) {
		t.Fatal("UI shell does not load the PNG favicon")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("/static/mark.jpg?v=1")) {
		t.Fatal("UI shell does not load the cache-busted mark")
	}
	for _, forbidden := range []string{"method=\"post\"", "type=\"hidden\""} {
		if bytes.Contains(rr.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("UI shell contains sensitive form transport: %q", forbidden)
		}
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("/static/ui.js")) {
		t.Fatal("UI shell does not load the browser controller")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Maximum 16 MiB")) {
		t.Fatal("UI shell does not document its client-side size limit")
	}
	for _, header := range []string{"Content-Security-Policy", "Referrer-Policy", "Permissions-Policy", "X-Frame-Options"} {
		if rr.Header().Get(header) == "" {
			t.Fatalf("missing %s", header)
		}
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("UI must not be cached")
	}

	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/s/example", nil))
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte("open-drop")) {
		t.Fatalf("reveal response: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAboutPage(t *testing.T) {
	srv, _ := testServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/about", nil))
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte("How it works")) {
		t.Fatalf("about response: %d %s", rr.Code, rr.Body.String())
	}
}

func TestStartupPage(t *testing.T) {
	srv, _ := testServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/startupz", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "started\n" {
		t.Fatalf("startup response: %d %q", rr.Code, rr.Body.String())
	}
}

func TestStaticAssetCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skin.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "favicon.ico"), []byte("ico"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := testServer(t)
	srv.cfg.StaticDir = dir
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/static/skin.css", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("css: %d", rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("css cache: %q", got)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/static/missing.css", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("missing cache: %q", got)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "ico" {
		t.Fatalf("root favicon: %d %q", rr.Code, rr.Body.String())
	}
}

// ensure Take-only: Get must not be used — compile-time checklist via this comment
// and parallel burn test above. Optional: spy store.
type takeOnlyStore struct {
	store.Store
	takes atomic.Int32
	gets  atomic.Int32
}

func (t *takeOnlyStore) Take(ctx context.Context, id string) (store.Record, error) {
	t.takes.Add(1)
	return t.Store.Take(ctx, id)
}

func (t *takeOnlyStore) Get(ctx context.Context, id string) (store.Record, error) {
	t.gets.Add(1)
	return t.Store.Get(ctx, id)
}

func TestGetUsesTakeOnly(t *testing.T) {
	dir := t.TempDir()
	inner, err := store.OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inner.Close() })
	spy := &takeOnlyStore{Store: inner}
	cfg := config.Config{
		MaxBytes: 1 << 20, DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, MinTTL: time.Minute,
		CreatePerIP: 100, CreateWindow: time.Hour, GetPerIP: 100, GetWindow: time.Hour,
	}
	srv := New(cfg, spy, nil)
	h := srv.Handler()
	pkg := sealBlob(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewReader(pkg))
	req.Header.Set("Content-Type", "application/octet-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var cr createResp
	_ = json.NewDecoder(rr.Body).Decode(&cr)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/secrets/"+cr.ID, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	if spy.takes.Load() != 1 {
		t.Fatalf("takes=%d", spy.takes.Load())
	}
	if spy.gets.Load() != 0 {
		t.Fatalf("Get was called %d times", spy.gets.Load())
	}
}
