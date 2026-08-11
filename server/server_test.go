package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
