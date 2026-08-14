#!/usr/bin/env bash
# Reproduce CI Browser UI in the official Playwright image.
# Prefer this over pushing to GitHub Actions.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

if command -v docker >/dev/null 2>&1; then
  ctr=docker
elif command -v podman >/dev/null 2>&1; then
  ctr=podman
else
  echo "need docker or podman to run the Playwright image locally" >&2
  exit 1
fi

pw_ver=$(node -p "require('./package.json').devDependencies['@playwright/test']")
image="${PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v${pw_ver}-noble}"

goroot=$(go env GOROOT)
gopath=$(go env GOPATH)
wasm_exec="${goroot}/lib/wasm/wasm_exec.js"
if [ ! -f "${wasm_exec}" ]; then
  wasm_exec="${goroot}/misc/wasm/wasm_exec.js"
fi
if [ ! -f "${wasm_exec}" ]; then
  echo "GOROOT=${goroot} has no wasm_exec.js (Debian split GOROOT?). Use the official toolchain from go.mod." >&2
  exit 1
fi

echo "Using ${ctr} image ${image}"
exec "$ctr" run --rm --ipc=host \
  -v "${root}:/work:Z" \
  -v "${goroot}:${goroot}:ro" \
  -v "${gopath}:${gopath}" \
  -w /work \
  -e HOME=/root \
  -e GOPATH="${gopath}" \
  -e GOROOT="${goroot}" \
  -e GOTOOLCHAIN=local \
  -e GOFLAGS=-buildvcs=false \
  -e PATH="${goroot}/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  "${image}" \
  bash -lc '
    set -euo pipefail
    mkdir -p web/static
    WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
    if [ ! -f "${WASM_EXEC}" ]; then
      WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
    fi
    cp "${WASM_EXEC}" web/static/wasm_exec.js
    GOOS=js GOARCH=wasm go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o web/static/dead-drop.wasm ./cmd/dead-drop-wasm
    npm ci
    npm run test:browser
  '
