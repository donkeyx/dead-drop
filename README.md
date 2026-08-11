# dead-drop

A dead drop for secrets: encrypt **before** upload, key in the URL fragment (`#...`). The server only ever sees ciphertext.

```
https://your.host/s/<id>#<key>
         ↑ server knows id     ↑ never sent to the server
```

## Status

| Piece | State |
|-------|--------|
| **Design** | [DESIGN.md](DESIGN.md) |
| **PR1 — SEAL blob library** | done (`blob/`) |
| Server / UI / WASM | next |

## Library (PR1)

```go
import "github.com/donkeyx/dead-drop/blob"

key, _ := blob.GenerateMasterKey()
pkg, err := blob.Seal([]byte("my secret"), key, blob.SealOptions{
    ContentType: "text/plain; charset=utf-8",
    // Passphrase: []byte("optional second factor"),
})
// ...
res, err := blob.Open(pkg, key, nil)
```

Golden interop vectors: `blob/testdata/v1_nopass.json`, `v1_passphrase.json` (CLI/WASM must open these byte-identical later).

```bash
go test ./...
make generate-vectors   # only if regenerating testdata
```

## Threat model (short)

| Server has | Server does not have |
|------------|----------------------|
| Ciphertext, TTL, burn flag | Plaintext |
| Size / timestamps | Fragment key (`#…`) |

**Not magic:** if you host the web UI, users still trust the code you serve. XSS or a malicious deploy can steal keys. Offline CLI encrypt is the paranoia path (PR2).

## Stack (planned)

- Go library (SEAL v1) + CLI
- Go **WASM** browser crypto (same code)
- HTMX UI chrome only
- MIT

## License

MIT — see [LICENSE](LICENSE).
