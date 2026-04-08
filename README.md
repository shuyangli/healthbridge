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

## Layout

```
cli/                 Go CLI (`healthbridge` binary)
ios/                 SwiftUI iOS app (HealthBridge)
relay/               Cloudflare Worker (TypeScript)
proto/               Shared JSON schemas — single source of truth for wire types
skill/healthbridge/  Agent skill package wrapping the CLI
```

## Install the CLI

```sh
brew install shuyangli/tap/healthbridge

healthbridge pair --relay https://healthbridge.shuyang-li.workers.dev
# scan the QR with the HealthBridge iOS app
```

Linux users and anyone who wants a tarball can grab one from
[GitHub Releases](https://github.com/shuyangli/healthbridge/releases).
Go developers can still `go install
github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest`.

See [`cli/README.md`](cli/README.md) for the full install, configure,
and first-run walkthrough.

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
- [x] M3 — Scopes + write path + audit log
  - `healthbridge write/scopes/status` subcommands with full test coverage
  - PairRecord.Scopes + per-pair scope validation
  - HealthBridgeKit AuditLog actor with FIFO cap + JSON persistence
  - PRIVACY.md and in-app Disclosure strings pinned for 5.1.2(i) compliance
- [x] M4 — Job queue surface + sync + cache
  - SQLite jobs mirror at $XDG_DATA_HOME/healthbridge/jobs.db with full lifecycle
  - `healthbridge jobs list/get/wait/cancel/prune` against the mirror
  - SQLite cache for samples + per-type anchors at cache.db
  - `healthbridge sync` with HKAnchoredObjectQuery semantics + multi-page reassembly
  - `healthbridge wipe` clears local state for a pair
  - Multi-page drainer in fakerelay; sync scenario tests cover adds, deletes, --full
- [x] M5 — Help, JSON, agent skill package
  - `healthbridge types` subcommand listing every supported sample type + canonical unit
  - skill/healthbridge/SKILL.md as the agent manifest with command reference, example dialogues, error codes, and the critical pending-vs-done contract
  - skill/healthbridge/examples/ and README.md with Claude Code install instructions
- [ ] M6 — Local-direct fallback (no-cloud mode) — deferred
- [ ] M7 — (Conditional) APNs silent push — deferred
