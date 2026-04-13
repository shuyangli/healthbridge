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

## Deploy your own

The relay is designed to be self-hosted — there is no shared
"healthbridge.example" instance. Each user runs their own Worker on
their own Cloudflare account. The relay only ever sees ciphertext
(M2+), so this is the safest deployment.

Cloudflare's free plan is sufficient — Durable Objects on the free
tier work as long as the DO classes are SQLite-backed, which this
relay already configures (see `wrangler.toml`'s
`new_sqlite_classes`).

```sh
# 1. Sign in once. Opens a browser to authorize wrangler against your
#    Cloudflare account.
npx wrangler login

# 2. Pick a unique Worker name. The default `healthbridge` will collide
#    with any other user's deploy on the same workers.dev subdomain;
#    change it to something specific to you.
$EDITOR wrangler.toml   # set `name = "healthbridge-<yourhandle>"`

# 3. Deploy.
npx wrangler deploy
```

`wrangler deploy` prints the deployed URL, e.g.
`https://healthbridge-<yourhandle>.<your-subdomain>.workers.dev`. That
is the value to pass as `--relay` to the CLI and to set as
`HEALTHBRIDGE_RELAY` in your shell.

Re-deploying picks up source changes; the per-pair Durable Object
state survives across deploys.

### Relay secret (optional)

By default anyone who knows your relay URL can initiate a new pairing.
To restrict pairing to people you approve, set a relay-level secret:

```sh
# Generate a random secret and store it as a Worker secret.
npx wrangler secret put RELAY_SECRET

# Set the same value in your shell so the CLI sends it during pairing.
export HEALTHBRIDGE_RELAY_SECRET="<same-value>"
```

When `RELAY_SECRET` is set, `POST /v1/pair` and `GET /v1/pair` require
a matching `X-Relay-Secret` header. The CLI reads the secret from
`HEALTHBRIDGE_RELAY_SECRET` and embeds it in the QR code so the iOS
app sends it automatically. Post-pairing endpoints are unaffected —
they use the per-pair Bearer token.

## API

```
POST   /v1/jobs?pair=<id>            { job_id, blob } → enqueue
GET    /v1/jobs?pair=<id>&since=N    long-poll for jobs whose seq > N
POST   /v1/results?pair=<id>         { job_id, page_index, blob } → post result
GET    /v1/results?pair=<id>&job_id=<j>   long-poll for result pages
DELETE /v1/pair?pair=<id>            wipe a pair
GET    /v1/health                    liveness ping
```

`pair` is a 26-character ULID established at pairing. Pairing endpoints
(`/v1/pair`) are optionally gated by a relay-level `X-Relay-Secret`
header. All other endpoints require a per-pair `Authorization: Bearer`
token issued at pairing completion.
