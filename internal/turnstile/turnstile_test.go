package turnstile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("secret") != "sec" || r.Form.Get("response") != "tok" {
			t.Fatalf("form=%v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	t.Cleanup(ts.Close)
	c := &Client{Secret: "sec", VerifyURL: ts.URL, HTTP: ts.Client()}
	if err := c.Verify("tok", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejects(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error-codes": []string{"invalid-input-response"}})
	}))
	t.Cleanup(ts.Close)
	c := &Client{Secret: "sec", VerifyURL: ts.URL, HTTP: ts.Client()}
	if err := c.Verify("bad", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyEmptyToken(t *testing.T) {
	c := &Client{Secret: "sec"}
	if err := c.Verify("  ", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestDisabledClient(t *testing.T) {
	var c *Client
	if err := c.Verify("", ""); err != nil {
		t.Fatal(err)
	}
}
