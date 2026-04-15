---
name: healthbridge
description: Read and write Apple Health data on the user's iPhone via the `healthbridge` CLI. Use this skill when the user asks to log nutrition (calories, water, macros), weight, or other HealthKit-tracked metrics, or when they ask about recent activity, workouts, sleep, or vitals. Wraps an end-to-end-encrypted relay between the Mac CLI and the HealthBridge iOS app — every job and result is sealed under a per-pair session key. Jobs only drain while the iPhone app is foregrounded; expect `pending` responses when the phone is asleep.
license: MIT
compatibility: Requires the `healthbridge` binary on PATH, a paired iPhone running the HealthBridge iOS app, and outbound HTTPS to the configured Cloudflare relay.
metadata:
  version: "0.1.0"
  source: https://github.com/shuyangli/healthbridge
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
| "Did the meal log apply?" | `healthbridge jobs wait <id>` |

## When NOT to invoke

- The user is asking about a non-Apple platform (Garmin, Google Fit,
  Fitbit). HealthBridge only knows about Apple HealthKit.
- The user wants you to *interpret* their data. Fetch with this skill
  first, then reason in your own response.

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

## Configuration

After `healthbridge pair`, the pair ID and relay URL are saved to
`~/.healthbridge/config` and used as defaults for all subsequent
commands. No env vars are required. The env vars `HEALTHBRIDGE_PAIR`
and `HEALTHBRIDGE_RELAY` still work as overrides if set.

## Command summary

Run `healthbridge help` for the full list. Key commands for the agent:

```
healthbridge read <type> [--from -7d] [--to now] [--limit N] --json
healthbridge write <type> --value <n> --unit <u> [--at <t>] [--meta k=v] --json
healthbridge profile <field> --json     # date_of_birth, biological_sex, blood_type, …
healthbridge jobs list|get|wait|cancel|prune
healthbridge status --json
healthbridge types --json
```

`healthbridge profile <field>` returns a single HealthKit characteristic
the user has set in the Health app: `date_of_birth` (ISO date),
`biological_sex` ("female" / "male" / "other"), `blood_type`
("a_positive", "ab_negative", …), `fitzpatrick_skin_type` ("type_i"
through "type_vi"), `wheelchair_use` ("yes" / "no"), or
`activity_move_mode` ("active_energy" / "apple_move_time"). An empty
`value` means the user hasn't set the field. Use these to ground
fitness-coaching answers — e.g. compute age from `date_of_birth`,
choose calorie estimates from `biological_sex`. **Never invent or
extrapolate these values; if the field is empty, ask the user.**

Detailed flags, JSON output shapes, sample-type catalog, and error
codes are in [references/COMMANDS.md](references/COMMANDS.md) and
[references/TYPES.md](references/TYPES.md). Load them on demand.

The catalog covers **every non-deprecated `HKQuantityTypeIdentifier`
Apple ships** — 119 quantity types across activity, body
measurements, vital signs, lab results, nutrition, hearing, mobility,
reproductive, UV, diving, alcohol, and sleep — plus `sleep_analysis`
and `workout`. If the user asks about a metric you don't recognise by
name, run `healthbridge types --json` to find the canonical wire
name; almost anything Health.app shows is in there. Read access is
requested for every type at pairing time. Write access is restricted
to the "agent could plausibly log this" set: macros + minerals +
vitamins, body weight / fat / lean / waist / height, glucose, blood
pressure, and alcoholic-beverage count.

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
summary.

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
- **Confirm before destructive ops.** `wipe` is a one-way door from
  the agent's perspective.

## Pairing is a user action

`healthbridge pair` shows a QR in the terminal that the user scans with
the HealthBridge iOS app. Never invoke `pair` from the agent — it's a
one-time human-in-the-loop ritual. If `healthbridge status --json`
returns `pair_incomplete` or no pair record exists, tell the user to
run `healthbridge pair` themselves.
