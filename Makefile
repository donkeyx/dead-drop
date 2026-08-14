.PHONY: test tidy build wasm wasm-test vectors generate-vectors test-browser-container

test:
	go test ./...

build:
	go build -o bin/dead-drop ./cmd/dead-drop

tidy:
	go mod tidy

# Browser crypto (Go → WASM). Needs a Go toolchain with wasm support.
wasm:
	mkdir -p web/static
	@WASM_EXEC=$$(go env GOROOT)/lib/wasm/wasm_exec.js; \
	  if [ ! -f "$$WASM_EXEC" ]; then WASM_EXEC=$$(go env GOROOT)/misc/wasm/wasm_exec.js; fi; \
	  cp "$$WASM_EXEC" web/static/wasm_exec.js
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
	  -o web/static/dead-drop.wasm ./cmd/dead-drop-wasm
	@echo "wasm size: $$(wc -c < web/static/dead-drop.wasm) bytes"
	@gzip -c web/static/dead-drop.wasm | wc -c | awk '{printf "wasm gzip: %s bytes\n", $$1}'

# Open PR1 golden vectors via Node + WASM (requires node + make wasm).
wasm-test: wasm
	node web/static/wasm_vectors.mjs

# Regenerate blob golden vectors (committed under blob/testdata/).
generate-vectors:
	GENERATE_VECTORS=1 go test ./blob/ -run TestGenerateVectors -count=1

vectors: generate-vectors test

# Same image as CI Browser UI / live smoke. Use this before pushing workflow changes.
test-browser-container:
	./scripts/test-browser-container.sh
