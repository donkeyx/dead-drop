# Design Document Summary — SealShare

**Output:** `/tmp/grok-1000/grok-design-doc-08e4b6f3.md`  
**Review:** `/tmp/grok-1000/grok-design-review-08e4b6f3.md` (22 + 4 residual issues addressed; open count 0)  
**Date:** 2026-08-11  
**Status:** Draft, revised after re-review (ready for PR1)

## What was produced

A full systems design for a **client-side encrypted secret/file sharing** service, codenamed **SealShare** (`github.com/donkeyx/sealshare`, MIT), revised to fix format contradictions, burn races, deploy constraints, and implementer gaps.

## Codename

**SealShare** — client seals content before upload; operator holds ciphertext only.

## Core design (post-review)

| Area | Choice |
|------|--------|
| Trust model | Client-side AEAD; server never sees plaintext or fragment keys |
| Product language | “client-side encrypted” / “operator cannot read plaintext”; “zero-knowledge storage” only with qualifier |
| URL | `https://host/s/{id}#{key}` (128-bit id, 256-bit key, base64url) |
| AEAD | XChaCha20-Poly1305; **always** HKDF-SHA256 domain separation |
| Passphrase | Optional Argon2id (default **32 MiB / t=2 / p=1** interactive) + HKDF mix |
| Blob | Normative SEAL v1: clear header = KDF only; name/MIME in encrypted envelope; **no IS_FILE** |
| Burn | **Atomic `Store.Take`** (burn delete inside Take; GET never Get-then-Delete); prefer lost body over dual fetch |
| Browser crypto | Go → WASM (no parallel TS crypto in v1) |
| UI | HTMX chrome + client trust-boundary checklist (PR7) |
| Server | stdlib `net/http`; **no CORS**; error JSON + `Retry-After` |
| Deploy v1 | **Single writer / single node**; data-dir lock; multi-replica unsupported |
| Storage | Store: Create / **`Take`** / optional Get / …; SQLite or FS + quarantine |
| Defaults | Burn ON, TTL 24h (max 7d), max 16 MiB |
| License | MIT |

## Review-driven changes (major)

1. Single normative blob layout + Appendix C (removed clear filename/MIME contradiction).
2. Atomic burn → **`Take` delivery** (round 2: no Get-then-Delete TOCTOU on handler composition).
3. Explicit single-node deployment constraints.
4. Lower interactive Argon2 memory (64 → 32 MiB) + runtime RAM budget.
5. Full API freeze (TTL grammar, errors, `?alt=json`, no CORS).
6. PR plan gates (vectors, burn race, WASM interop); rate limits in PR4; split S3 vs burn token (PR9/PR10).
7. Round 2: deterministic envelope JSON; Open rejects flags/N mismatches; FS quarantine lifecycle.

## Document sections

Overview, goals, architecture + Mermaid, normative blob format, sequences, REST API, Store + deploy constraints, WASM, CLI, threat model, observability, alternatives, **Key Decisions** (26), Open Questions, **PR Plan** (PR1–PR10), Appendices A–D.

## Implementation target (not done)

Greenfield: `/home/david/mywork/repos/sealshare/` — design only.

## Next step

**PR1:** SEAL v1 `blob` library + positive/negative golden vectors (Appendix B). HTTP GET must use **`Take` only** (PR3/PR4).
