# SealShare

Client-side encrypted secret & small-file sharing. The server only stores ciphertext; the decryption key lives in the URL fragment (`#...`), so a normal host never sees it.

**Status:** design complete, implementation starting (see [DESIGN.md](DESIGN.md)).

## Planned stack

- Go library (SEAL blob format) + CLI
- Go **WASM** for browser crypto (same code as CLI)
- HTMX UI chrome (not crypto)
- MIT license

## Docs

- **[DESIGN.md](DESIGN.md)** — threat model, blob format, API, WASM, PR plan

## License

MIT (planned; LICENSE lands with the first implementation PR).
