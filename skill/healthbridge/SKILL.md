---
name: healthbridge
description: Read and write Apple Health data on the user's iPhone via the `healthbridge` CLI. Use this skill when the user asks to log nutrition (calories, water, macros), weight, or other HealthKit-tracked metrics, or when they ask about recent activity, workouts, sleep, or vitals. Wraps an end-to-end-encrypted relay between the Mac CLI and the HealthBridge iOS app — every job and result is sealed under a per-pair session key. Jobs only drain while the iPhone app is foregrounded; expect `pending` responses when the phone is asleep.
license: MIT
compatibility: Requires the `healthbridge` binary on PATH, a paired iPhone running the HealthBridge iOS app, and outbound HTTPS to the configured Cloudflare relay.
metadata:
  version: "0.1.0"
  source: https://github.com/shuyangli/apple-health-cli
---

# healthbridge

Drives the `healthbridge` CLI, which talks to the HealthBridge iPhone app
via a tiny serverless relay. The relay is a dumb mailbox that sees only
ciphertext; the iPhone is the only place plaintext HealthKit data exists.

## When to invoke

| User intent | Command |
|---|---|
| "Log a 500 kcal lunch" | `healthbridge write dietary_energy_consumed --value 500 --unit kcal --at now` |
| "Drink of water (250 mL)" | `healthbridge write dietary_water --value 250 --unit mL --at now` |
| "I weigh 73.2 kg" | `healthbridge write body_mass --value 73.2 --unit kg --at now` |
| "Steps yesterday?" | `healthbridge read step_count --from -1d --to now` |
| "Resting heart rate this week" | `healthbridge read heart_rate_resting --from -7d` |
| "Pull all my recent workouts" | `healthbridge sync --type workout` |
| "Did the meal log apply?" | `healthbridge jobs wait <id>` |

## When NOT to invoke

- The user is asking about a non-Apple platform (Garmin, Google Fit,
  Fitbit). HealthBridge only knows about Apple HealthKit.
- The user wants you to *interpret* their data. Fetch with this skill
  first, then reason in your own response.
- The user asks to delete every sample, revoke a pair, or wipe the
  cache. Confirm explicitly before running `wipe` or destructive
  `scopes revoke`.

## Always pass `--json`

Every command supports `--json` for machine-readable output. Use it.
Every response includes a `status` field.

## Critical contract: pending vs done

| status | meaning | what to tell the user |
|---|---|---|
| `done` | iPhone executed the job; result is back. | "Logged X." |
| `pending` | Relay accepted the job; iPhone hasn't drained it yet. | "Queued — will apply next time you open HealthBridge. Job ID: `<id>`." |
| `failed` | iPhone tried and refused. `error.code` + `error.message` explain. | Surface the error. |

**Never claim a write applied if `status == "pending"`.** Remember the
`job_id` and offer to follow up with `healthbridge jobs wait <id>`.

## The two required env vars

To avoid passing `--pair` and `--relay` on every call, the user should set:

```sh
export HEALTHBRIDGE_PAIR=01J...        # ULID of the paired iPhone
export HEALTHBRIDGE_RELAY=https://...  # base URL of the relay
```

If `HEALTHBRIDGE_PAIR` is unset, `healthbridge status --json` will list
the pair records on disk; pick the most recent one.

## Command summary

```
healthbridge read <type> [--from -7d] [--to now] [--limit N] --json
healthbridge write <type> --value <n> --unit <u> [--at <t>] [--meta k=v] --json
healthbridge sync [--type <t>...] [--full] --json
healthbridge jobs list|get|wait|cancel|prune
healthbridge status --json
healthbridge scopes list|grant|revoke
healthbridge types --json
healthbridge pair                       # user-only; never invoke from agent
healthbridge wipe [--yes]                # destructive; confirm first
```

Detailed flags, JSON output shapes, sample-type catalog, and error
codes are in [references/COMMANDS.md](references/COMMANDS.md) and
[references/TYPES.md](references/TYPES.md). Load them on demand.

## Worked examples

### Logging breakfast (happy path)

> User: "I had a 350 kcal granola bowl for breakfast"

```sh
healthbridge write dietary_energy_consumed \
  --value 350 --unit kcal --at -3h \
  --meta description="granola bowl" --meta source=agent --json
```

```json
{ "job_id": "01J...", "status": "done", "uuid": "<healthkit-uuid>" }
```

> Agent: "Logged 350 kcal of granola bowl to Health."

### Logging breakfast (iPhone offline)

Same command, response is:

```json
{ "job_id": "01J...", "status": "pending" }
```

> Agent: "I queued the write — it'll apply the next time you open
> HealthBridge on your phone. The job ID is `01J...` if you want me to
> check on it later."

### Following up on a pending write

> User: "Did the breakfast log apply yet?"

```sh
healthbridge jobs wait 01J... --timeout 30s --json
```

If `status == "done"`, confirm. If still `pending`, tell the user the
phone is still asleep.

### Reading recent activity

> User: "How was my walking this week?"

```sh
healthbridge read step_count --from -7d --json
```

Group the returned `samples[]` by day, sum, and present a short prose
summary. For larger backfills, prefer `healthbridge sync --type
step_count` (anchored delta query, much cheaper on repeat calls).

### Logging a meal with macros

For a meal with calories *and* macros, write each macro as a separate
sample with the same `--at` timestamp so HealthKit groups them:

```sh
healthbridge write dietary_energy_consumed --value 620 --unit kcal --at now --json
healthbridge write dietary_protein         --value 38  --unit g    --at now --json
healthbridge write dietary_carbohydrates   --value 72  --unit g    --at now --json
healthbridge write dietary_fat_total       --value 18  --unit g    --at now --json
```

## Privacy & safety

- **The relay sees ciphertext only.** Don't send PII in `--meta` fields
  expecting it to be private — assume it lands on the user's iPhone in
  plaintext as part of the audit log.
- **The iPhone keeps an audit log** of every job you run. The user can
  review and revoke at any time.
- **Fetch only what you need.** A 7-day step query is fine; pulling
  every heart-rate sample for a year is rude.
- **Confirm before destructive ops.** `wipe` and `scopes revoke` are
  one-way doors from the agent's perspective.

## Pairing is a user action

`healthbridge pair` shows a QR in the terminal that the user scans with
the HealthBridge iOS app. Never invoke `pair` from the agent — it's a
one-time human-in-the-loop ritual. If `healthbridge status --json`
returns `pair_incomplete` or no pair record exists, tell the user to
run `healthbridge pair` themselves.
