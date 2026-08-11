# dead-drop

A dead drop for secrets: encrypt **before** upload, key in the URL fragment (`#...`). The server only ever sees ciphertext.

**Status:** design complete — see [DESIGN.md](DESIGN.md). Implementation starting with the SEAL blob library (PR1).

```
https://your.host/s/<id>#<key>
         ↑ server knows id     ↑ never sent to the server
```

## Stack (planned)

- Go library (SEAL format) + CLI
- Go **WASM** browser crypto (same code as CLI)
- HTMX for UI chrome only
- MIT

## Docs

- **[DESIGN.md](DESIGN.md)** — threat model, blob format, API, WASM, PR plan

## License

MIT (LICENSE with first implementation PR).
