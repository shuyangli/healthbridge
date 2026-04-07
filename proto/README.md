# Wire protocol

Single source of truth for the JSON shapes that flow between the CLI, the
relay, and the iOS app. This package is intentionally tiny — types are written
out by hand in each language so we don't take a code-generator dependency.

## Layers

The wire format has three nested layers:

```
┌─ relay envelope (HTTP request/response, plaintext) ─────────┐
│  POST /jobs body:                                           │
│  { "pair_id": "...", "blob": "<base64 ciphertext>" }        │
│                                                             │
│  ┌─ encrypted job blob (M2+; M1 is plaintext) ──────────┐   │
│  │  XChaCha20-Poly1305(plaintext, key, nonce, aad)      │   │
│  │                                                      │   │
│  │  ┌─ job plaintext (this file) ─────────────────┐     │   │
│  │  │  { "id": "...", "kind": "read", ... }       │     │   │
│  │  └──────────────────────────────────────────────┘     │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

In M1 there is no encryption — the "blob" field is just a base64-encoded JSON
job plaintext. M2 introduces the cipher layer; nothing else changes.

## Job kinds

| kind     | meaning |
|----------|---------|
| `read`   | Synchronous read of a single sample type within a time range. |
| `write`  | Append one sample to HealthKit. |
| `sync`   | Anchored delta query for one or more sample types. (M4) |

## Status values

A job goes through `pending → running → done | failed | expired`. The CLI's
local job mirror tracks this; the relay does not.

See `proto/schema.json` for the full schema.
