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
| **PR2 — offline CLI** | done (`cmd/dead-drop` seal/open) |
| **PR3 — store + Take** | done (`store/` FS + SQLite) |
| **PR4 — HTTP API** | done (`server/`, `dead-drop serve`) |
| **PR5 — network put/get** | done |
| **PR6 — WASM crypto** | done (`make wasm` / `make wasm-test`) |
| **PR7 — browser UI shell** | in progress (create/reveal pages, same-origin JS/WASM) |

## CLI (offline)

```bash
go build -o bin/dead-drop ./cmd/dead-drop

# seal a file → package + key (keep the key offline)
./bin/dead-drop seal -in secret.txt -out secret.seal -key-out secret.key

# open
./bin/dead-drop open -in secret.seal -out secret.txt -key-file secret.key

# optional passphrase (from env, never argv)
export DEADDROP_PASS='correct horse'
./bin/dead-drop seal -in f -out f.seal -key-out k -passphrase-env DEADDROP_PASS
./bin/dead-drop open -in f.seal -out f -key-file k -passphrase-env DEADDROP_PASS
```

## Server + network CLI

```bash
./bin/dead-drop serve -addr :8080 -data ./data -store sqlite

# shared storage for multiple replicas / Kubernetes HPA
DEADDROP_STORE=postgres \
DEADDROP_DATABASE_URL='postgres://user:password@postgres/deaddrop?sslmode=require' \
./bin/dead-drop serve -addr :8080

# seal client-side, upload ciphertext, print share link (includes #key)
./bin/dead-drop put -server http://127.0.0.1:8080 -in secret.txt

# download + decrypt (flags before URL)
./bin/dead-drop get -out secret.txt 'http://127.0.0.1:8080/s/ID#KEY'

# or raw API (ciphertext only)
curl -sS -X POST http://127.0.0.1:8080/api/v1/secrets \
  -H 'Content-Type: application/octet-stream' \
  -H 'X-Seal-TTL: 24h' -H 'X-Seal-Burn: 1' \
  --data-binary @secret.seal
```

No CORS. Fragment keys never go to the server.

## Browser WASM

```bash
make wasm        # web/static/dead-drop.wasm + wasm_exec.js
make wasm-test   # Node harness opens PR1 golden vectors
```

```html
<script src="/static/wasm_exec.js"></script>
<script src="/static/deaddrop.js"></script>
<script>
  const { blob, key } = await DeadDrop.encrypt(new TextEncoder().encode("hi"), {
    contentType: "text/plain",
  });
  // POST blob to /api/v1/secrets; share URL + "#" + key
  const { plaintext } = await DeadDrop.decrypt(blob, key);
</script>
```

Gzip size target ≤ 1.5 MiB (currently ~1.0 MiB).

## Browser UI

The server serves the create shell at `/` and the reveal shell at `/s/{id}`.
Build the generated WASM assets before starting the server:

```bash
make wasm
./bin/dead-drop serve -addr :8080 -data ./data -static ./web/static
```

The browser encrypts before `POST /api/v1/secrets`; the fragment key is never sent to the server. Text and small files up to 16 MiB are supported, with files downloaded using their encrypted filename and content type. Reveal pages clear the fragment from the visible address bar on first use, but burn-after-read still consumes the drop when the ciphertext is fetched.

## Deployment storage

SQLite and filesystem storage are single-writer backends for one replica with one local data directory. Use `DEADDROP_STORE=postgres` with `DEADDROP_DATABASE_URL` before running multiple replicas or enabling Kubernetes HPA. PostgreSQL keeps burn-after-read `Take` atomic across replicas; the database must be reachable by every pod and protected with TLS and normal secret management.

The current rate limiter is in-memory and therefore per-pod. PostgreSQL makes secret storage safe across replicas, but shared/global rate limiting is a separate deployment-hardening task before treating HPA limits as fleet-wide limits.

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

Golden interop vectors: `blob/testdata/v1_nopass.json`, `v1_passphrase.json`.

GitHub Actions runs Go formatting, tests, race tests, vet, a static CLI build, `govulncheck`, WASM asset generation, browser JavaScript syntax checks, vector interop, and the compressed WASM size gate.

GitHub Actions also runs Playwright browser tests for text drops, file drops, burn-after-read behavior, and fragment-free API requests. Locally, install dependencies with `npm ci`, install Chromium and its host dependencies with `npx playwright install --with-deps chromium`, then run `npm run test:browser`.

```bash
go test ./...
make build
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
