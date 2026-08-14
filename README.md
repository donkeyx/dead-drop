# dead-drop

```
╭──────────────────────────────────────╮
│   🐴 DonkeyX's dead-drop             │
╰──────────────────────────────────────╯

        //\\
       (/oo\)   .--------.
       (____)  | SEALED  |
        /||\   '--------'
       //||\\   📦 burn after read
      ^^ ^^ ^^
   "Encrypt first. Leave the key in the fragment."
```

A dead drop for secrets. The browser (or CLI) encrypts **before** upload. The key lives in the URL fragment (`#...`), which never goes to the server. All the operator holds is ciphertext.

Handy when a password-manager share isn't an option — an API token, a private key, a small kubeconfig — or you just need to move a secret between your own devices. Don't paste it into Slack or Discord and use the channel as a clipboard; those histories keep a copy. Burn-after-read and a short TTL are on by default so the drop doesn't hang around.

Don't want to run anything? Use **[drop.donkeyx.dev](https://drop.donkeyx.dev/)** in the browser. Self-host if you don't want to trust this origin (see [Not magic](#not-magic)).

```
https://your.host/s/<id>#<key>
         ↑ server knows id     ↑ never sent to the server
```

Same donkey stable as [tcp-wait](https://github.com/donkeyx/tcp-wait) / [cluster-utils-api](https://github.com/donkeyx/cluster-utils-api) — this one’s the “pass a secret without the host reading it” bit.

| hosted | https://drop.donkeyx.dev/ |
| dockerhub | https://hub.docker.com/r/donkeyx/dead-drop |
| ghcr | `ghcr.io/donkeyx/dead-drop` |
| helm | `oci://ghcr.io/donkeyx/charts/dead-drop` |
| design | [DESIGN.md](DESIGN.md) |

## Leave a drop

**Browser:** [drop.donkeyx.dev](https://drop.donkeyx.dev/) — type a secret or attach a file (max 16 MiB), copy the link. Same UI if you self-host. Encryption runs in WASM in the page. The server response is an id and path only — no fragment key.

**CLI** (seal offline, or put over the network):

```bash
go build -o bin/dead-drop ./cmd/dead-drop

# keep the key on your machine
./bin/dead-drop seal -in secret.txt -out secret.seal -key-out secret.key
./bin/dead-drop open -in secret.seal -out secret.txt -key-file secret.key

# or seal client-side, upload ciphertext, print the share link (includes #key)
./bin/dead-drop put -server https://drop.donkeyx.dev -in secret.txt
./bin/dead-drop get -out secret.txt 'https://drop.donkeyx.dev/s/ID#KEY'
```

Passphrases come from the environment, never argv:

```bash
export DEADDROP_PASS='correct horse'
./bin/dead-drop seal -in f -out f.seal -key-out k -passphrase-env DEADDROP_PASS
```

Burn-after-read is **on** by default. A concurrent burn `Take` has one winner; a failed response after `Take` still consumes the drop.

## Not magic

| Server has | Server does not have |
|------------|----------------------|
| Ciphertext, TTL, burn flag | Plaintext |
| Size / timestamps | Fragment key (`#…`) |

If you use the **hosted UI**, you still trust the JS/WASM we serve. XSS or a malicious deploy can steal keys. The offline CLI is the paranoia path. Self-host if you do not trust this origin.

No CORS. No accounts. No “zero-knowledge” badge — just client-side encryption and an operator who cannot read the disk.

## Run your own

Single node (SQLite or filesystem — one writer, one data dir):

```bash
make wasm
./bin/dead-drop serve -addr :8080 -data ./data -store sqlite
```

Replicas need Postgres (`DEADDROP_STORE=postgres` + `DEADDROP_DATABASE_URL`). That keeps `Take` atomic across pods. The rate limiter is still per-pod.

```bash
docker pull ghcr.io/donkeyx/dead-drop:latest
docker pull docker.io/donkeyx/dead-drop:latest
```

Helm chart, probes, and the values overlay: [deploy/helm/dead-drop/README.md](deploy/helm/dead-drop/README.md). Hub creds live on the `ci` environment. Cluster deploys use a separate `production` environment (`v*` tags, or **Run workflow** with that semver) — see the chart README.

```bash
kubectl create secret generic dead-drop-db \
  -n dead-drop \
  --from-literal=database-url='postgres://deaddrop:password@postgres.example/deaddrop?sslmode=require'

cp deploy/helm/dead-drop/values.example.yaml deploy/helm/dead-drop/values.local.yaml
helm upgrade --install dead-drop oci://ghcr.io/donkeyx/charts/dead-drop \
  --version 0.1.1 \
  -n dead-drop --create-namespace \
  -f deploy/helm/dead-drop/values.local.yaml
```

Health: `GET /healthz`, `GET /startupz`, `GET /readyz`.

## Library

Same SEAL v1 code the CLI and WASM use:

```go
import "github.com/donkeyx/dead-drop/blob"

key, _ := blob.GenerateMasterKey()
pkg, err := blob.Seal([]byte("my secret"), key, blob.SealOptions{
    ContentType: "text/plain; charset=utf-8",
    // Passphrase: []byte("optional second factor"),
})
res, err := blob.Open(pkg, key, nil)
```

Golden vectors: `blob/testdata/v1_nopass.json`, `v1_passphrase.json`.

## Develop

```bash
go test ./...
make build
make wasm        # web/static/dead-drop.wasm + wasm_exec.js
make wasm-test   # Node harness opens the golden vectors
```

CI runs format, tests, race, vet, `govulncheck`, WASM size (gzip ≤ 1.5 MiB, currently ~1.0), and Playwright (text drop, file drop, burn, no fragment on the wire). Locally: `npm ci && npx playwright install --with-deps chromium && npm run test:browser`.

## License

MIT — see [LICENSE](LICENSE).
