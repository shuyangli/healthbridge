---
name: healthbridge
description: |
  Read and write Apple Health data on the user's iPhone via the
  healthbridge CLI. Use this skill when the user asks to log nutrition,
  weight, or other HealthKit-tracked things, or when they ask about
  their recent activity, workouts, sleep, or vitals. The CLI talks to
  a tiny serverless relay that brokers encrypted job blobs between the
  Mac and the user's iPhone — the iPhone has to be foregrounded (or
  recently active) for jobs to drain.
---

# healthbridge — Apple Health CLI skill

This skill wraps the `healthbridge` binary, which is the desktop side
of the HealthBridge project. The iPhone runs a companion app
("HealthBridge") that owns access to HealthKit. The two ends are paired
via an X25519 exchange and every job/result that crosses the relay is
encrypted with the per-pair session key — the relay never sees plaintext
Health data.

## When to invoke this skill

- "Log a 500 kcal lunch" → `healthbridge write dietary_energy_consumed`
- "How many steps did I take yesterday?" → `healthbridge read step_count`
- "Pull all my recent workouts" → `healthbridge sync --type workout`
- "What did I queue up earlier?" → `healthbridge jobs list`
- "Did the breakfast log actually apply?" → `healthbridge jobs wait <id>`

## When NOT to invoke this skill

- The user asks about Health data on a different platform (Garmin,
  Google Fit, Fitbit). HealthBridge only knows about Apple HealthKit.
- The user wants to draw conclusions or coach decisions from raw data
  — fetch with this skill first, then reason in your own response.
- The user asks to do something destructive (delete every sample,
  revoke a pair). Use the targeted commands and confirm first.

## Critical contract: pending vs done

Every command can return one of these statuses in its JSON output:

| status    | meaning                                                                 |
|-----------|-------------------------------------------------------------------------|
| `done`    | The iPhone executed the job and the result is back. Tell the user.    |
| `pending` | The relay accepted the job but the iPhone hasn't drained it yet.      |
| `failed`  | The iPhone tried and refused. The `error` field has a code + message. |

**Never tell the user "I logged X" if the status is `pending`.** Instead
say "I queued the write — it will apply the next time you open
HealthBridge on your phone (job `<id>`)" and remember the job_id so you
can follow up. Use `healthbridge jobs wait <id>` later in the same
conversation if the user wants confirmation.

## Command reference

All commands accept `--json` (recommended for agent use) and require
`--pair <ulid>` to identify which paired iPhone to talk to. Set
`HEALTHBRIDGE_PAIR` in your environment to avoid passing it every time.

### `healthbridge read <type>`

Read recent samples for one HealthKit type.

```sh
healthbridge read step_count --from -7d --to now --json
healthbridge read heart_rate_resting --from 2026-04-01 --json
```

JSON output:

```json
{
  "job_id": "abc123...",
  "status": "done",
  "type": "step_count",
  "samples": [
    { "type": "step_count", "value": 8421, "unit": "count",
      "start": "2026-04-06T00:00:00Z", "end": "2026-04-07T00:00:00Z" }
  ]
}
```

### `healthbridge write <type>`

Append one sample to HealthKit.

```sh
healthbridge write dietary_energy_consumed --value 500 --unit kcal --at now --json
healthbridge write body_mass --value 73.2 --unit kg --at 2026-04-07T08:00:00Z --json
healthbridge write dietary_water --value 250 --unit mL --meta source=manual --json
```

Required flags: `--value`, `--unit`. The `--at` and `--end` flags accept
RFC3339 (`2026-04-07T12:30:00Z`), `YYYY-MM-DD`, `now`, or relative
offsets like `-1h`, `-30m`, `-7d`. Repeat `--meta key=value` to attach
arbitrary metadata.

JSON output on success:

```json
{ "job_id": "...", "status": "done", "uuid": "<healthkit-uuid>" }
```

JSON output on pending:

```json
{ "job_id": "...", "status": "pending" }
```

### `healthbridge sync [--type <t>] [--full]`

Pull anchored deltas of HealthKit samples into the local SQLite cache.
Use this for bulk operations: "give me everything for the last month"
is much better served by `sync` than by repeated `read` calls.

```sh
healthbridge sync --json                  # all supported types
healthbridge sync --type workout --json   # one type
healthbridge sync --full --type body_mass # wipe anchor and re-pull
```

JSON output reports `added` and `deleted` counts. Subsequent `sync`
calls only fetch deltas since the last anchor; expect them to be cheap.

### `healthbridge jobs list|get|wait|cancel|prune`

Inspect the local job mirror — the CLI's record of every job it has
sent to the iPhone.

```sh
healthbridge jobs list --status pending
healthbridge jobs get <id>
healthbridge jobs wait <id> --timeout 60s
healthbridge jobs cancel <id>
healthbridge jobs prune --age 720h
```

`wait` long-polls the relay; if the job is still pending after the
timeout, the row stays pending and you can call `wait` again later.

### `healthbridge status`

Show the pair record + scopes + relay reachability.

```sh
healthbridge status --json
```

### `healthbridge scopes list|grant|revoke`

Manage the per-pair set of allowed sample types.

```sh
healthbridge scopes list
healthbridge scopes grant dietary_energy_consumed
healthbridge scopes revoke body_mass
```

### `healthbridge types`

Print the supported sample types and their canonical units.

```sh
healthbridge types --json
```

### `healthbridge pair`

Run the X25519 pairing protocol against an iOS-app-supplied link.
Almost never invoked from an agent — the user runs this once per Mac,
by hand, after scanning a QR code shown on their iPhone.

### `healthbridge wipe`

Delete every local trace of a pair. Destructive — confirm with the
user before invoking, and pass `--yes` only if you're sure.

## Example dialogues

### Logging breakfast (happy path)

> User: "I had a 350 kcal granola bowl for breakfast"
>
> Agent: runs `healthbridge write dietary_energy_consumed --value 350
> --unit kcal --at -3h --meta source=agent --json`
>
> Output: `{ "job_id": "01J...", "status": "done", "uuid": "..." }`
>
> Agent: "Logged 350 kcal of granola bowl to your Health app."

### Logging breakfast (iPhone offline)

> User: "Add a 350 kcal granola bowl"
>
> Agent: runs the same command
>
> Output: `{ "job_id": "01J...", "status": "pending" }`
>
> Agent: "I queued the write — it'll apply the next time you open
> HealthBridge on your phone. The job ID is `01J...` if you want me to
> check on it later."

### Following up on a pending write

> User: "Did the breakfast log apply yet?"
>
> Agent: runs `healthbridge jobs wait 01J... --timeout 30s --json`
>
> Output: `{ "status": "done", ... }`
>
> Agent: "Yes — it just landed."

### Recent activity summary

> User: "How was my walking this week?"
>
> Agent: runs `healthbridge read step_count --from -7d --json`
>
> Then summarises the daily totals from the response.

## Error codes

When `status` is `failed` or the CLI itself returns a non-zero exit
code, the JSON includes `{ "code": "...", "message": "..." }`. Common
codes:

| code              | meaning                                            |
|-------------------|----------------------------------------------------|
| `pair_incomplete` | Pairing has not finished. Tell the user to pair.   |
| `bad_auth`        | The local pair record is stale. Re-pair.           |
| `scope_denied`    | The requested type isn't in the granted scope set. |
| `mailbox_full`    | The relay queue is at capacity. Try later.         |
| `anchor_invalidated` | The cache is stale; sync --full will recover.   |

## Privacy reminder

The user's health data flows from the iPhone, through an encrypted
relay, to the CLI on the Mac. The relay sees only ciphertext. The
iPhone keeps an audit log of every job you execute. Be respectful:
fetch only what you need, and don't read or write health data that
hasn't been authorised by the user.
