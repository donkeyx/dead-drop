// Package blob implements the SEAL v1 ciphertext package format for dead-drop.
//
// Encryption is client-side only: the server treats package bytes as opaque
// after size/magic/version checks. See DESIGN.md for the normative layout.
package blob

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	Magic       = "SEAL"
	VersionV1   = 0x01
	FlagHasPass = 0x01

	MasterKeySize = 32
	SaltSize      = 16
	NonceSize     = chacha20poly1305.NonceSizeX // 24
	TagSize       = 16
	HeaderPassN   = 26 // salt_len + salt + time + mem + para

	MaxMetaLen  = 1024
	MaxNameLen  = 255
	MaxArgonMem = 1_048_576 // KiB (1 GiB)
	MaxArgonT   = 10
	MaxArgonP   = 16

	// InteractiveWASM is the browser/default Argon2id profile (header-stored).
	InteractiveArgonTime = 2
	InteractiveArgonMem  = 32_768 // KiB = 32 MiB
	InteractiveArgonPara = 1

	hkdfInfo = "deaddrop-v1/aead"
)

var (
	ErrInvalidPackage   = errors.New("blob: invalid package")
	ErrUnsupportedVer   = errors.New("blob: unsupported version")
	ErrDecryptFailed    = errors.New("blob: decryption failed")
	ErrInvalidKey       = errors.New("blob: invalid master key length")
	ErrInvalidOptions   = errors.New("blob: invalid options")
	ErrPassphraseNeeded = errors.New("blob: passphrase required")
	ErrArgonParams      = errors.New("blob: argon2 parameters out of range")
)

// SealOptions configures Seal. Zero Argon fields use InteractiveWASM defaults.
type SealOptions struct {
	// Passphrase is optional. Empty/nil = fragment key only.
	// Callers SHOULD zero after Seal returns.
	Passphrase  []byte
	Filename    string
	ContentType string
	ArgonTime   uint32 // 0 = default
	ArgonMem    uint32 // KiB; 0 = default
	ArgonPara   uint8  // 0 = default
}

// OpenResult is the decrypted payload and envelope metadata.
type OpenResult struct {
	Plaintext   []byte
	Filename    string
	ContentType string
}

// envelopeMeta is the encrypted JSON envelope (deterministic field order).
type envelopeMeta struct {
	V    int    `json:"v"`
	CT   string `json:"ct"`
	Name string `json:"name"`
}

// Seal encrypts plaintext under masterKey (32 bytes) and returns a SEAL v1 package.
// masterKey is the fragment key material (not the final AEAD key).
func Seal(plaintext []byte, masterKey []byte, opt SealOptions) ([]byte, error) {
	return seal(plaintext, masterKey, opt, nil, nil)
}

// seal is the core implementation. fixedSalt (16) and fixedNonce (24), when non-nil,
// replace CSPRNG draws (used only to publish golden vectors / tests).
func seal(plaintext []byte, masterKey []byte, opt SealOptions, fixedSalt, fixedNonce []byte) ([]byte, error) {
	if len(masterKey) != MasterKeySize {
		return nil, ErrInvalidKey
	}
	if err := validateName(opt.Filename); err != nil {
		return nil, err
	}
	if fixedSalt != nil && len(fixedSalt) != SaltSize {
		return nil, ErrInvalidOptions
	}
	if fixedNonce != nil && len(fixedNonce) != NonceSize {
		return nil, ErrInvalidOptions
	}

	ct := opt.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	metaJSON, err := json.Marshal(envelopeMeta{V: 1, CT: ct, Name: opt.Filename})
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > MaxMetaLen {
		return nil, fmt.Errorf("%w: meta too large", ErrInvalidOptions)
	}

	// plaintext envelope
	env := make([]byte, 2+len(metaJSON)+len(plaintext))
	binary.BigEndian.PutUint16(env[0:2], uint16(len(metaJSON)))
	copy(env[2:], metaJSON)
	copy(env[2+len(metaJSON):], plaintext)

	hasPass := len(opt.Passphrase) > 0
	var (
		flags   byte
		header  []byte
		aeadKey []byte
	)

	if hasPass {
		flags = FlagHasPass
		time, mem, para, err := argonParams(opt)
		if err != nil {
			return nil, err
		}
		salt := make([]byte, SaltSize)
		if fixedSalt != nil {
			copy(salt, fixedSalt)
		} else if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
		header = make([]byte, HeaderPassN)
		header[0] = SaltSize
		copy(header[1:17], salt)
		binary.BigEndian.PutUint32(header[17:21], time)
		binary.BigEndian.PutUint32(header[21:25], mem)
		header[25] = para

		aeadKey, err = deriveKey(masterKey, opt.Passphrase, salt, time, mem, para)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		aeadKey, err = deriveKey(masterKey, nil, nil, 0, 0, 0)
		if err != nil {
			return nil, err
		}
	}
	defer zeroBytes(aeadKey)

	n := uint16(len(header))
	// framing without nonce/ct yet
	prefix := make([]byte, 8+len(header))
	copy(prefix[0:4], Magic)
	prefix[4] = VersionV1
	prefix[5] = flags
	binary.BigEndian.PutUint16(prefix[6:8], n)
	copy(prefix[8:], header)
	aad := prefix // first 8+N bytes

	nonce := make([]byte, NonceSize)
	if fixedNonce != nil {
		copy(nonce, fixedNonce)
	} else if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	aead, err := chacha20poly1305.NewX(aeadKey)
	if err != nil {
		return nil, err
	}
	ctAndTag := aead.Seal(nil, nonce, env, aad)

	out := make([]byte, 0, len(prefix)+NonceSize+len(ctAndTag))
	out = append(out, prefix...)
	out = append(out, nonce...)
	out = append(out, ctAndTag...)
	return out, nil
}

// Open decrypts a SEAL v1 package with masterKey and optional passphrase.
func Open(packageBytes []byte, masterKey []byte, passphrase []byte) (OpenResult, error) {
	var zero OpenResult
	if len(masterKey) != MasterKeySize {
		return zero, ErrInvalidKey
	}
	// minimum: magic+ver+flags+N + nonce + tag (empty plaintext envelope still has meta)
	if len(packageBytes) < 8+NonceSize+TagSize {
		return zero, ErrInvalidPackage
	}
	if string(packageBytes[0:4]) != Magic {
		return zero, ErrInvalidPackage
	}
	ver := packageBytes[4]
	if ver != VersionV1 {
		return zero, ErrUnsupportedVer
	}
	flags := packageBytes[5]
	if flags&^FlagHasPass != 0 {
		return zero, ErrInvalidPackage
	}
	n := binary.BigEndian.Uint16(packageBytes[6:8])
	if int(8+n+NonceSize+TagSize) > len(packageBytes) {
		return zero, ErrInvalidPackage
	}

	switch flags {
	case 0x00:
		if n != 0 {
			return zero, ErrInvalidPackage
		}
	case FlagHasPass:
		if n != HeaderPassN {
			return zero, ErrInvalidPackage
		}
	default:
		return zero, ErrInvalidPackage
	}

	header := packageBytes[8 : 8+n]
	aad := packageBytes[:8+n]
	nonce := packageBytes[8+n : 8+n+NonceSize]
	ctAndTag := packageBytes[8+n+NonceSize:]

	var aeadKey []byte
	var err error
	if flags&FlagHasPass != 0 {
		if len(passphrase) == 0 {
			return zero, ErrPassphraseNeeded
		}
		if header[0] != SaltSize {
			return zero, ErrInvalidPackage
		}
		salt := header[1:17]
		time := binary.BigEndian.Uint32(header[17:21])
		mem := binary.BigEndian.Uint32(header[21:25])
		para := header[25]
		if err := checkArgonParams(time, mem, para); err != nil {
			return zero, err
		}
		aeadKey, err = deriveKey(masterKey, passphrase, salt, time, mem, para)
	} else {
		aeadKey, err = deriveKey(masterKey, nil, nil, 0, 0, 0)
	}
	if err != nil {
		return zero, err
	}
	defer zeroBytes(aeadKey)

	aead, err := chacha20poly1305.NewX(aeadKey)
	if err != nil {
		return zero, err
	}
	plain, err := aead.Open(nil, nonce, ctAndTag, aad)
	if err != nil {
		return zero, ErrDecryptFailed
	}
	if len(plain) < 2 {
		return zero, ErrInvalidPackage
	}
	metaLen := binary.BigEndian.Uint16(plain[0:2])
	if int(2+metaLen) > len(plain) || metaLen > MaxMetaLen {
		return zero, ErrInvalidPackage
	}
	metaRaw := plain[2 : 2+metaLen]
	payload := plain[2+metaLen:]

	// Liberal JSON: require v, ct, name; ignore extras.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(metaRaw, &raw); err != nil {
		return zero, ErrInvalidPackage
	}
	var meta envelopeMeta
	if v, ok := raw["v"]; !ok {
		return zero, ErrInvalidPackage
	} else if err := json.Unmarshal(v, &meta.V); err != nil || meta.V != 1 {
		return zero, ErrInvalidPackage
	}
	if v, ok := raw["ct"]; !ok {
		return zero, ErrInvalidPackage
	} else if err := json.Unmarshal(v, &meta.CT); err != nil {
		return zero, ErrInvalidPackage
	}
	if v, ok := raw["name"]; !ok {
		return zero, ErrInvalidPackage
	} else if err := json.Unmarshal(v, &meta.Name); err != nil {
		return zero, ErrInvalidPackage
	}
	if err := validateName(meta.Name); err != nil {
		return zero, err
	}

	// Copy payload so caller doesn't share internal buffer if we zero later.
	out := make([]byte, len(payload))
	copy(out, payload)
	return OpenResult{
		Plaintext:   out,
		Filename:    meta.Name,
		ContentType: meta.CT,
	}, nil
}

// GenerateMasterKey returns 32 cryptographically random bytes for a fragment key.
func GenerateMasterKey() ([]byte, error) {
	k := make([]byte, MasterKeySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// EncodeKeyB64URL encodes key material for URL fragments.
func EncodeKeyB64URL(key []byte) string {
	return b64urlEncode(key)
}

// DecodeKeyB64URL decodes fragment key material.
func DecodeKeyB64URL(s string) ([]byte, error) {
	return b64urlDecode(s)
}

func deriveKey(masterKey, passphrase, salt []byte, time, mem uint32, para uint8) ([]byte, error) {
	info := []byte(hkdfInfo)
	var hkdfSalt []byte
	if len(passphrase) > 0 {
		passKey := argon2.IDKey(passphrase, salt, time, mem, para, 32)
		defer zeroBytes(passKey)
		hkdfSalt = passKey
	} else {
		hkdfSalt = make([]byte, 32) // zeros
	}
	r := hkdf.New(sha256.New, masterKey, hkdfSalt, info)
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

func argonParams(opt SealOptions) (time, mem uint32, para uint8, err error) {
	time = opt.ArgonTime
	mem = opt.ArgonMem
	para = opt.ArgonPara
	if time == 0 {
		time = InteractiveArgonTime
	}
	if mem == 0 {
		mem = InteractiveArgonMem
	}
	if para == 0 {
		para = InteractiveArgonPara
	}
	err = checkArgonParams(time, mem, para)
	return
}

func checkArgonParams(time, mem uint32, para uint8) error {
	if time == 0 || time > MaxArgonT {
		return ErrArgonParams
	}
	if mem == 0 || mem > MaxArgonMem {
		return ErrArgonParams
	}
	if para == 0 || para > MaxArgonP {
		return ErrArgonParams
	}
	return nil
}

func validateName(name string) error {
	if len(name) > MaxNameLen {
		return fmt.Errorf("%w: name too long", ErrInvalidOptions)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: name not utf-8", ErrInvalidOptions)
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("%w: name contains path separator or NUL", ErrInvalidOptions)
	}
	return nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
	// prevent compiler from optimizing away (best-effort)
	if len(b) > 0 {
		_ = subtle.ConstantTimeByteEq(b[0], 0)
	}
}
