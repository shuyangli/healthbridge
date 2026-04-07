# HealthBridge

A CLI + iOS app + serverless relay that lets local AI agents read and write
Apple Health data.

The desktop CLI (`healthbridge`, in [`cli/`](cli/)) talks to a tiny Cloudflare
Worker relay (in [`relay/`](relay/)). The iOS companion app (in [`ios/`](ios/))
owns HealthKit and drains the relay's encrypted job mailbox whenever it is
foregrounded.

The relay is dumb store-and-forward and never sees plaintext Health data —
all jobs and results are end-to-end encrypted between the CLI and the iOS app
with a key established at pairing time.

See [`/Users/shuyangli/.claude/plans/curried-strolling-hanrahan.md`](/Users/shuyangli/.claude/plans/curried-strolling-hanrahan.md)
for the full design.

## Layout

```
cli/                 Go CLI (`healthbridge` binary)
ios/                 SwiftUI iOS app (HealthBridge)
relay/               Cloudflare Worker (TypeScript)
proto/               Shared JSON schemas — single source of truth for wire types
skill/healthbridge/  Agent skill package wrapping the CLI
```

## Status

Implementation in progress. Milestones:

- [x] M1 — Relay skeleton + walking-skeleton read (plaintext)
  - Cloudflare Worker relay with per-pair Durable Object mailbox + 36 vitest tests
  - Go CLI with `read` subcommand, relay client, jobs codec, scenario tests
  - HealthBridgeKit Swift package with relay client + codecs + 9 XCTest tests
  - SwiftUI app skeleton (HealthBridgeApp/) with HealthKit drain loop
- [x] M2 — Encryption + real pairing
  - X25519 + HKDF + ChaCha20-Poly1305 in cli/internal/crypto + Swift CryptoKit mirror
  - Cross-language fixture tests prove byte-identical interop (RFC 7748 vectors + custom)
  - Session-based jobs codec with AAD binding (pair_id, job_id[, page_index])
  - /v1/pair endpoint on the relay; X25519 exchange via PairAsCLI / PairAsIOS helpers
  - `healthbridge pair` subcommand with QR-link parsing + 6-digit SAS confirmation
  - Per-pair Bearer auth_token enforced on every authenticated relay request
  - 50 relay vitest tests, 21 Swift XCTest tests, full Go test suite race-clean
- [ ] M3 — Scopes + write path + audit log
- [ ] M3 — Scopes + write path + audit log
- [ ] M4 — Job queue surface + sync + cache
- [ ] M5 — Help, JSON, agent skill package
- [ ] M6 — Local-direct fallback (no-cloud mode)
- [ ] M7 — (Conditional) APNs silent push
