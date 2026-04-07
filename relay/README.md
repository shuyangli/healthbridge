# healthbridge-relay

A tiny Cloudflare Worker that brokers encrypted job blobs between the
`healthbridge` CLI and the iOS HealthBridge app. Each pair has its own
Durable Object mailbox; the relay never inspects or stores plaintext.

## Layout

```
src/
  index.ts          Worker entry — routes pair_id → Durable Object
  durable_object.ts Per-pair DO that wraps a Mailbox + persistence
  handler.ts        HTTP routing layer (pure TS, no Workers types)
  mailbox.ts        In-memory job/result store (pure TS, no Workers types)
  *.test.ts         Vitest tests for the pure-TS layers
```

The pure-TS layers (`mailbox.ts`, `handler.ts`) are unit-tested in plain
Node and have no Workers dependency. The Workers wrapper (`durable_object.ts`,
`index.ts`) is a thin glue layer that loads/persists state and routes
requests to the right DO instance.

## Develop

```sh
npm install
npm test          # vitest, no Workers runtime needed
npx wrangler dev  # spawns workerd locally on http://127.0.0.1:8787
```

## API

```
POST   /v1/jobs?pair=<id>            { job_id, blob } → enqueue
GET    /v1/jobs?pair=<id>&since=N    long-poll for jobs whose seq > N
POST   /v1/results?pair=<id>         { job_id, page_index, blob } → post result
GET    /v1/results?pair=<id>&job_id=<j>   long-poll for result pages
DELETE /v1/pair?pair=<id>            wipe a pair
GET    /v1/health                    liveness ping
```

`pair` is a 26-character ULID established at pairing. M1 has no auth; M2
adds per-pair HMAC tokens.
