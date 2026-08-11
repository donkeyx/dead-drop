package blob

import "encoding/base64"

func b64urlEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	// Accept both raw and padded.
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
