package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donkeyx/dead-drop/internal/config"
	"github.com/donkeyx/dead-drop/server"
	"github.com/donkeyx/dead-drop/store"
)

func TestCLIPutGetLoopback(t *testing.T) {
	// real HTTP server on loopback
	dir := t.TempDir()
	st, err := store.OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Config{
		MaxBytes: 1 << 20, DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, MinTTL: time.Minute,
		CreatePerIP: 100, CreateWindow: time.Hour, GetPerIP: 100, GetWindow: time.Hour,
	}
	srv := server.New(cfg, st, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	bin := buildCLI(t)
	tmp := t.TempDir()
	in := filepath.Join(tmp, "plain.txt")
	out := filepath.Join(tmp, "out.txt")
	mustWrite(t, in, []byte("network round trip secret"))

	// put (link on stdout only)
	cmd := exec.Command(bin, "put", "-server", ts.URL, "-in", in, "-burn=true", "-ttl", "1h")
	var putStdout, putStderr strings.Builder
	cmd.Stdout = &putStdout
	cmd.Stderr = &putStderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("put: %v\nstderr=%s\nstdout=%s", err, putStderr.String(), putStdout.String())
	}
	link := strings.TrimSpace(putStdout.String())
	if !strings.Contains(link, "/s/") || !strings.Contains(link, "#") {
		t.Fatalf("no share link in stdout:\n%s\nstderr:\n%s", putStdout.String(), putStderr.String())
	}

	// get — flags before positional URL (Go flag stops at first non-flag)
	cmd = exec.Command(bin, "get", "-out", out, link)
	getOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("get: %v\n%s", err, getOut)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "network round trip secret" {
		t.Fatalf("got %q", got)
	}

	// burn: second get fails
	cmd = exec.Command(bin, "get", link, "-out", filepath.Join(tmp, "out2.txt"))
	if err := cmd.Run(); err == nil {
		t.Fatal("expected second get to fail after burn")
	}
}
