package blob

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Run with: GENERATE_VECTORS=1 go test ./blob/ -run TestGenerateVectors
func TestGenerateVectors(t *testing.T) {
	if os.Getenv("GENERATE_VECTORS") != "1" {
		t.Skip("set GENERATE_VECTORS=1 to regenerate testdata")
	}

	// Fixed materials for interop.
	mk := bytesRepeat(0x42, 32)
	nonce0 := bytesRepeat(0xA1, 24)
	nonce1 := bytesRepeat(0xB2, 24)
	salt := bytesRepeat(0xC3, 16)

	// nopass
	pt0 := []byte("hello dead-drop secret")
	blob0, err := seal(pt0, mk, SealOptions{
		Filename:    "",
		ContentType: "text/plain; charset=utf-8",
	}, nil, nonce0)
	if err != nil {
		t.Fatal(err)
	}
	writeGolden(t, "v1_nopass.json", golden{
		Version:      1,
		MasterKeyB64: EncodeKeyB64URL(mk),
		PlaintextB64: EncodeKeyB64URL(pt0),
		BlobB64:      EncodeKeyB64URL(blob0),
		Filename:     "",
		ContentType:  "text/plain; charset=utf-8",
		NonceB64:     EncodeKeyB64URL(nonce0),
	})

	// passphrase
	pt1 := []byte("passphrase protected payload")
	pass := "test-passphrase-for-vectors"
	blob1, err := seal(pt1, mk, SealOptions{
		Passphrase:  []byte(pass),
		Filename:    "secret.txt",
		ContentType: "text/plain; charset=utf-8",
		ArgonTime:   InteractiveArgonTime,
		ArgonMem:    InteractiveArgonMem,
		ArgonPara:   InteractiveArgonPara,
	}, salt, nonce1)
	if err != nil {
		t.Fatal(err)
	}
	writeGolden(t, "v1_passphrase.json", golden{
		Version:      1,
		MasterKeyB64: EncodeKeyB64URL(mk),
		Passphrase:   pass,
		PlaintextB64: EncodeKeyB64URL(pt1),
		BlobB64:      EncodeKeyB64URL(blob1),
		Filename:     "secret.txt",
		ContentType:  "text/plain; charset=utf-8",
		SaltB64:      EncodeKeyB64URL(salt),
		NonceB64:     EncodeKeyB64URL(nonce1),
		ArgonTime:    InteractiveArgonTime,
		ArgonMem:     InteractiveArgonMem,
		ArgonPara:    InteractiveArgonPara,
	})
}

func writeGolden(t *testing.T, name string, g golden) {
	t.Helper()
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("testdata", name)
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
