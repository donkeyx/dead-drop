package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donkeyx/dead-drop/blob"
)

// Ensures the built CLI opens PR1 golden vectors byte-identically.
func TestCLIOpenGoldenVectors(t *testing.T) {
	bin := buildCLI(t)

	// nopass
	g := loadGoldenFile(t, "v1_nopass.json")
	dir := t.TempDir()
	sealPath := filepath.Join(dir, "n.seal")
	outPath := filepath.Join(dir, "n.out")
	keyPath := filepath.Join(dir, "n.key")
	mustWrite(t, sealPath, mustDecode(t, g.BlobB64))
	mustWrite(t, keyPath, []byte(g.MasterKeyB64+"\n"))

	cmd := exec.Command(bin, "open", "-in", sealPath, "-out", outPath, "-key-file", keyPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open nopass: %v\n%s", err, out)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	want := mustDecode(t, g.PlaintextB64)
	if string(got) != string(want) {
		t.Fatalf("plaintext mismatch")
	}

	// passphrase
	gp := loadGoldenFile(t, "v1_passphrase.json")
	sealP := filepath.Join(dir, "p.seal")
	outP := filepath.Join(dir, "p.out")
	keyP := filepath.Join(dir, "p.key")
	mustWrite(t, sealP, mustDecode(t, gp.BlobB64))
	mustWrite(t, keyP, []byte(gp.MasterKeyB64+"\n"))

	cmd = exec.Command(bin, "open", "-in", sealP, "-out", outP, "-key-file", keyP, "-passphrase-env", "TEST_PASS")
	cmd.Env = append(os.Environ(), "TEST_PASS="+gp.Passphrase)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open pass: %v\n%s", err, out)
	}
	got, err = os.ReadFile(outP)
	if err != nil {
		t.Fatal(err)
	}
	want = mustDecode(t, gp.PlaintextB64)
	if string(got) != string(want) {
		t.Fatalf("passphrase plaintext mismatch")
	}
}

func TestCLISealOpenRoundTrip(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "plain.txt")
	seal := filepath.Join(dir, "x.seal")
	key := filepath.Join(dir, "x.key")
	out := filepath.Join(dir, "out.txt")
	mustWrite(t, in, []byte("cli round trip secret"))

	cmd := exec.Command(bin, "seal", "-in", in, "-out", seal, "-key-out", key, "-ct", "text/plain")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seal: %v\n%s", err, o)
	}
	cmd = exec.Command(bin, "open", "-in", seal, "-out", out, "-key-file", key)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open: %v\n%s", err, o)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "cli round trip secret" {
		t.Fatalf("got %q", got)
	}
}

type goldenFile struct {
	MasterKeyB64 string `json:"master_key_b64url"`
	Passphrase   string `json:"passphrase"`
	PlaintextB64 string `json:"plaintext_b64url"`
	BlobB64      string `json:"blob_b64url"`
}

func loadGoldenFile(t *testing.T, name string) goldenFile {
	t.Helper()
	// testdata lives in blob/testdata relative to module root
	b, err := os.ReadFile(filepath.Join("..", "..", "blob", "testdata", name))
	if err != nil {
		// when running as go test ./cmd/dead-drop, cwd is package dir
		b, err = os.ReadFile(filepath.Join("blob", "testdata", name))
	}
	if err != nil {
		// find from module root via relative from this file
		root := findModuleRoot(t)
		b, err = os.ReadFile(filepath.Join(root, "blob", "testdata", name))
	}
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "dead-drop")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(findModuleRoot(t), "cmd", "dead-drop")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := blob.DecodeKeyB64URL(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
