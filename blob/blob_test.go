package blob

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type golden struct {
	Version      int    `json:"version"`
	MasterKeyB64 string `json:"master_key_b64url"`
	Passphrase   string `json:"passphrase,omitempty"`
	PlaintextB64 string `json:"plaintext_b64url"`
	BlobB64      string `json:"blob_b64url"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SaltB64      string `json:"salt_b64url,omitempty"`
	NonceB64     string `json:"nonce_b64url,omitempty"`
	ArgonTime    uint32 `json:"argon_time,omitempty"`
	ArgonMem     uint32 `json:"argon_mem_kib,omitempty"`
	ArgonPara    uint8  `json:"argon_para,omitempty"`
}

func loadGolden(t *testing.T, name string) golden {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGoldenNoPass(t *testing.T) {
	g := loadGolden(t, "v1_nopass.json")
	mk, err := DecodeKeyB64URL(g.MasterKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := DecodeKeyB64URL(g.PlaintextB64)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DecodeKeyB64URL(g.BlobB64)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := DecodeKeyB64URL(g.NonceB64)
	if err != nil {
		t.Fatal(err)
	}

	got, err := seal(pt, mk, SealOptions{
		Filename:    g.Filename,
		ContentType: g.ContentType,
	}, nil, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("blob mismatch\n got %s\nwant %s", EncodeKeyB64URL(got), g.BlobB64)
	}

	res, err := Open(want, mk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Plaintext, pt) {
		t.Fatalf("plaintext mismatch")
	}
	if res.Filename != g.Filename || res.ContentType != g.ContentType {
		t.Fatalf("meta: %+v", res)
	}
}

func TestHasPassphrase(t *testing.T) {
	nopass := loadGolden(t, "v1_nopass.json")
	pass := loadGolden(t, "v1_passphrase.json")
	nopassBlob, err := DecodeKeyB64URL(nopass.BlobB64)
	if err != nil {
		t.Fatal(err)
	}
	passBlob, err := DecodeKeyB64URL(pass.BlobB64)
	if err != nil {
		t.Fatal(err)
	}
	if HasPassphrase(nopassBlob) {
		t.Fatal("nopass flagged")
	}
	if !HasPassphrase(passBlob) {
		t.Fatal("passphrase package not flagged")
	}
	if HasPassphrase(nil) || HasPassphrase([]byte("SEAL")) {
		t.Fatal("short package flagged")
	}
}

func TestGoldenPassphrase(t *testing.T) {
	g := loadGolden(t, "v1_passphrase.json")
	mk, err := DecodeKeyB64URL(g.MasterKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := DecodeKeyB64URL(g.PlaintextB64)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DecodeKeyB64URL(g.BlobB64)
	if err != nil {
		t.Fatal(err)
	}
	salt, err := DecodeKeyB64URL(g.SaltB64)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := DecodeKeyB64URL(g.NonceB64)
	if err != nil {
		t.Fatal(err)
	}

	got, err := seal(pt, mk, SealOptions{
		Passphrase:  []byte(g.Passphrase),
		Filename:    g.Filename,
		ContentType: g.ContentType,
		ArgonTime:   g.ArgonTime,
		ArgonMem:    g.ArgonMem,
		ArgonPara:   g.ArgonPara,
	}, salt, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("blob mismatch\n got %s\nwant %s", EncodeKeyB64URL(got), g.BlobB64)
	}

	res, err := Open(want, mk, []byte(g.Passphrase))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Plaintext, pt) {
		t.Fatalf("plaintext mismatch")
	}
}

func TestRoundTripRandom(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	pt := []byte("super secret token value")
	blob, err := Seal(pt, mk, SealOptions{
		Filename:    "",
		ContentType: "text/plain; charset=utf-8",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Open(blob, mk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Plaintext, pt) {
		t.Fatal("round trip failed")
	}
}

func TestRoundTripPassphrase(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	pt := []byte("with passphrase")
	pass := []byte("correct horse battery staple")
	blob, err := Seal(pt, mk, SealOptions{
		Passphrase:  pass,
		Filename:    "note.txt",
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(blob, mk, nil); err == nil {
		t.Fatal("expected passphrase required")
	}
	if _, err := Open(blob, mk, []byte("wrong")); err == nil {
		t.Fatal("expected decrypt fail")
	}
	res, err := Open(blob, mk, pass)
	if err != nil {
		t.Fatal(err)
	}
	if res.Filename != "note.txt" || !bytes.Equal(res.Plaintext, pt) {
		t.Fatalf("%+v", res)
	}
}

func TestNegativeCases(t *testing.T) {
	g := loadGolden(t, "v1_nopass.json")
	mk, _ := DecodeKeyB64URL(g.MasterKeyB64)
	blob, _ := DecodeKeyB64URL(g.BlobB64)
	pt, _ := DecodeKeyB64URL(g.PlaintextB64)

	// wrong master key
	badKey := append([]byte{}, mk...)
	badKey[len(badKey)-1] ^= 0xff
	if _, err := Open(blob, badKey, nil); err == nil {
		t.Fatal("wrong key should fail")
	}

	// truncated tag
	short := blob[:len(blob)-16]
	if _, err := Open(short, mk, nil); err == nil {
		t.Fatal("truncated tag should fail")
	}

	// truncated mid
	if _, err := Open(blob[:len(blob)/2], mk, nil); err == nil {
		t.Fatal("truncated mid should fail")
	}

	// flip magic
	m := append([]byte{}, blob...)
	m[3] = 'M'
	if _, err := Open(m, mk, nil); err == nil {
		t.Fatal("bad magic")
	}

	// wrong version
	v := append([]byte{}, blob...)
	v[4] = 0x02
	if _, err := Open(v, mk, nil); err == nil {
		t.Fatal("bad version")
	}

	// reserved flag
	f := append([]byte{}, blob...)
	f[5] = 0x02
	if _, err := Open(f, mk, nil); err == nil {
		t.Fatal("reserved flag")
	}

	// flags=0 with N!=0
	nfix := append([]byte{}, blob...)
	binary.BigEndian.PutUint16(nfix[6:8], 26)
	// pad fake header so length checks pass enough to hit flag rule
	pad := make([]byte, 26)
	nfix = append(nfix[:8], append(pad, nfix[8:]...)...)
	if _, err := Open(nfix, mk, nil); err == nil {
		t.Fatal("flags=0 N!=0")
	}

	// path in filename rejected on seal
	if _, err := Seal(pt, mk, SealOptions{Filename: "../x"}); err == nil {
		t.Fatal("path name seal")
	}

	// empty package
	if _, err := Open(nil, mk, nil); err == nil {
		t.Fatal("empty")
	}
	if _, err := Open([]byte("noise"), mk, nil); err == nil {
		t.Fatal("noise")
	}

	// passphrase negatives from golden
	gp := loadGolden(t, "v1_passphrase.json")
	mkp, _ := DecodeKeyB64URL(gp.MasterKeyB64)
	blobp, _ := DecodeKeyB64URL(gp.BlobB64)
	if _, err := Open(blobp, mkp, []byte("wrong-pass")); err == nil {
		t.Fatal("wrong passphrase")
	}

	// flags=1 with N=0
	fp := append([]byte{}, blobp...)
	fp[5] = FlagHasPass
	binary.BigEndian.PutUint16(fp[6:8], 0)
	// strip header would break layout; just set N=0 keeping rest — Open should reject before AEAD
	if _, err := Open(fp, mkp, []byte(gp.Passphrase)); err == nil {
		t.Fatal("flags=1 N=0")
	}

	// flip salt in clear header (AAD + KDF)
	fs := append([]byte{}, blobp...)
	fs[9] ^= 0xff // first salt byte (offset 8+1)
	if _, err := Open(fs, mkp, []byte(gp.Passphrase)); err == nil {
		t.Fatal("flipped salt")
	}
}

func TestMetaJSONDeterministic(t *testing.T) {
	// Same inputs → same meta bytes inside envelope when nonce/salt fixed.
	mk := bytesRepeat(0x11, 32)
	nonce := bytesRepeat(0x22, 24)
	pt := []byte("x")
	a, err := seal(pt, mk, SealOptions{ContentType: "text/plain", Filename: ""}, nil, nonce)
	if err != nil {
		t.Fatal(err)
	}
	b, err := seal(pt, mk, SealOptions{ContentType: "text/plain", Filename: ""}, nil, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("not deterministic")
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
