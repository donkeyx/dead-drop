//go:build js && wasm

// Command dead-drop-wasm exposes SEAL encrypt/decrypt to the browser via syscall/js.
// Build: make wasm
package main

import (
	"syscall/js"

	"github.com/donkeyx/dead-drop/blob"
)

func main() {
	api := map[string]any{
		"encrypt": js.FuncOf(jsEncrypt),
		"decrypt": js.FuncOf(jsDecrypt),
		"ready":   true,
	}
	js.Global().Set("deaddrop", js.ValueOf(api))
	// Keep the Go runtime alive.
	select {}
}

// encrypt(plaintext: Uint8Array, options?: object) -> { blob, key, flags }
func jsEncrypt(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError("encrypt: plaintext Uint8Array required")
	}
	pt, err := copyBytesFromJS(args[0])
	if err != nil {
		return jsError(err.Error())
	}
	opt := blob.SealOptions{}
	if len(args) >= 2 && args[1].Type() == js.TypeObject {
		o := args[1]
		if v := o.Get("passphrase"); v.Truthy() {
			opt.Passphrase = []byte(v.String())
		}
		if v := o.Get("filename"); v.Truthy() {
			opt.Filename = v.String()
		}
		if v := o.Get("contentType"); v.Truthy() {
			opt.ContentType = v.String()
		}
	}
	if opt.ContentType == "" {
		opt.ContentType = "application/octet-stream"
	}

	masterKey, err := blob.GenerateMasterKey()
	if err != nil {
		return jsError(err.Error())
	}
	pkg, err := blob.Seal(pt, masterKey, opt)
	// best-effort wipe passphrase copy
	for i := range opt.Passphrase {
		opt.Passphrase[i] = 0
	}
	if err != nil {
		return jsError(err.Error())
	}

	flags := 0
	if len(pkg) > 5 {
		flags = int(pkg[5])
	}

	return map[string]any{
		"blob":  bytesToUint8Array(pkg),
		"key":   blob.EncodeKeyB64URL(masterKey),
		"flags": flags,
	}
}

// decrypt(blob: Uint8Array, key: string, passphrase?: string)
func jsDecrypt(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return jsError("decrypt: blob and key required")
	}
	pkg, err := copyBytesFromJS(args[0])
	if err != nil {
		return jsError(err.Error())
	}
	keyStr := args[1].String()
	masterKey, err := blob.DecodeKeyB64URL(keyStr)
	if err != nil || len(masterKey) != blob.MasterKeySize {
		return jsError("invalid key")
	}
	var pass []byte
	if len(args) >= 3 && args[2].Truthy() {
		pass = []byte(args[2].String())
	}
	res, err := blob.Open(pkg, masterKey, pass)
	for i := range pass {
		pass[i] = 0
	}
	if err != nil {
		return jsError(err.Error())
	}
	return map[string]any{
		"plaintext":   bytesToUint8Array(res.Plaintext),
		"filename":    res.Filename,
		"contentType": res.ContentType,
	}
}

func copyBytesFromJS(v js.Value) ([]byte, error) {
	if v.Type() != js.TypeObject {
		return nil, errStr("expected Uint8Array")
	}
	n := v.Get("byteLength")
	if n.Type() != js.TypeNumber {
		// try length
		n = v.Get("length")
	}
	size := n.Int()
	if size < 0 {
		return nil, errStr("invalid length")
	}
	buf := make([]byte, size)
	js.CopyBytesToGo(buf, v)
	return buf, nil
}

func bytesToUint8Array(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

type jsErr struct{ msg string }

func (e jsErr) Error() string { return e.msg }

func errStr(s string) error { return jsErr{s} }

func jsError(msg string) any {
	return map[string]any{"error": msg}
}
