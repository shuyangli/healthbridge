# healthbridge — full command reference

Detailed flags and JSON output shapes for every `healthbridge`
subcommand. Load this when you need a flag the [SKILL.md](../SKILL.md)
summary doesn't cover.

## Global flags

| Flag | Purpose |
|---|---|
| `--pair <ulid>` | Which paired iPhone to talk to. Falls back to `HEALTHBRIDGE_PAIR`. |
| `--relay <url>` | Relay base URL. Falls back to `HEALTHBRIDGE_RELAY`, then `http://127.0.0.1:8787`. |
| `--wait <duration>` | How long to long-poll for a result before returning `pending`. Defaults to ~5s in interactive mode, 0s otherwise. |
| `--json` | Emit machine-readable JSON. Always pass this from an agent. |

## `read <type>`

Enqueues a read job for one HealthKit sample type and waits for the
iPhone to drain it.

```
healthbridge read <type> [flags]
```

| Flag | Default | Notes |
|---|---|---|
| `--from <ts>` | `-1d` | RFC3339, `YYYY-MM-DD`, `now`, or relative offset (`-7d`, `-6h`, `-30m`). |
| `--to <ts>` | `now` | Same format as `--from`. |
| `--limit <n>` | `0` | Cap on samples returned. 0 = no cap. |

JSON output (status=done):

```json
{
  "job_id": "01J...",
  "status": "done",
  "type": "step_count",
  "samples": [
    {
      "uuid": "ABCD-1234-...",
      "type": "step_count",
      "value": 8421,
      "unit": "count",
      "start": "2026-04-06T00:00:00Z",
      "end":   "2026-04-07T00:00:00Z",
      "source": { "name": "iPhone", "bundle_id": "com.apple.health" }
    }
  ]
}
```

JSON output (status=pending): `{ "job_id": "...", "status": "pending" }`

## `write <type>`

Saves one `HKQuantitySample` to HealthKit on the iPhone.

```
healthbridge write <type> --value <n> --unit <u> [flags]
```

| Flag | Required | Notes |
|---|---|---|
| `--value <n>` | yes | Numeric value. |
| `--unit <u>` | yes | HealthKit unit string (`kcal`, `kg`, `g`, `mg`, `mL`, `count`, `count/min`, `mg/dL`, …). The canonical unit per type is in [TYPES.md](TYPES.md). |
| `--at <ts>` | no (default `now`) | Sample timestamp. RFC3339 / `YYYY-MM-DD` / `now` / relative offset. |
| `--end <ts>` | no | End timestamp for ranged samples. Defaults to `--at` if omitted. |
| `--meta k=v` | no, repeatable | Arbitrary metadata key/value pairs attached to the sample. |

JSON output (success): `{ "job_id": "...", "status": "done", "uuid": "<healthkit-uuid>" }`

JSON output (pending): `{ "job_id": "...", "status": "pending" }`

## `sync`

Bulk delta-pull. Each sample type has its own `HKAnchoredObjectQuery`
anchor stored in the local SQLite cache; subsequent `sync` calls only
fetch new and deleted samples since the last anchor.

```
healthbridge sync [flags]
```

| Flag | Default | Notes |
|---|---|---|
| `--type <t>...` | all supported | Limit sync to specific sample types. |
| `--full` | off | Wipe anchors before syncing. Forces a full re-pull of every type listed. Rarely needed. |

JSON output:

```json
{
  "job_id": "01J...",
  "status": "done",
  "results": [
    { "type": "step_count", "added": 14, "deleted": 0 },
    { "type": "heart_rate", "added": 142, "deleted": 1 }
  ]
}
```

Prefer `sync` over repeated `read` for backfills longer than a day.

## `jobs`

Inspects the local SQLite mirror of every job this CLI has enqueued.
The mirror persists across CLI invocations, so this is the right
surface for following up on `pending` jobs from earlier in the
conversation.

```
healthbridge jobs list   [--status pending|done|failed|canceled] [--limit N]
healthbridge jobs get    <job_id>
healthbridge jobs wait   <job_id> [--timeout 60s]
healthbridge jobs cancel <job_id>
healthbridge jobs prune  [--age 720h]
```

`wait` long-polls the relay; if the job is still pending after the
timeout, the row stays `pending` and you can call `wait` again later.

`cancel` only marks the local mirror — it does not stop the iPhone from
draining the job if it has already been delivered.

`prune` removes terminal-state rows older than `--age` (default
`720h` = 30 days).

## `status`

Reads the local pair record, prints relay reachability, granted scopes,
and (in M4+) queue depth.

```
healthbridge status --json
```

JSON output:

```json
{
  "pair_id": "01J...",
  "relay_url": "https://...workers.dev",
  "relay_reachable": true,
  "scopes": ["step_count", "dietary_energy_consumed", ...],
  "paired_at": "2026-04-07T08:23:14Z"
}
```

Use this to discover which pair IDs exist on disk if `HEALTHBRIDGE_PAIR`
is unset.

## `scopes`

Manage the per-pair allow-list of sample types this CLI can read or
write. Stored in `~/.config/healthbridge/pairs/<pair_id>.json`.

```
healthbridge scopes list
healthbridge scopes grant <type> [<type>...]
healthbridge scopes revoke <type> [<type>...]
```

Empty scope list = grant everything (the default after pairing).

The iPhone re-validates every operation against its own copy of the
scopes, so revocations on the CLI side are advisory until the M3
consent ledger lands.

## `types`

Print every supported sample type and its canonical write unit.

```
healthbridge types --json
```

Always check this if you're unsure whether a type is supported. The
catalog also lives in [TYPES.md](TYPES.md), but `types --json` is the
authoritative source for the binary you're actually running.

## `pair`

Mints a pair_id, runs the X25519 key exchange against the relay, and
shows a QR code in the terminal. The user opens HealthBridge on the
iPhone, scans the QR, and confirms the SAS on both sides.

```
healthbridge pair [--wait 2m]
```

**Never invoke `pair` from an agent context.** It's a one-time
human-in-the-loop ritual that requires a physical iPhone in front of
the terminal. If pairing is broken, ask the user to run it themselves.

## `wipe`

Deletes everything this CLI knows about a pair: SQLite cache rows, job
mirror entries, and the pair record on disk.

```
healthbridge wipe [--yes]
```

Confirm explicitly with the user before invoking. `--yes` skips the
prompt — use it only after you've gotten the user's confirmation.

`wipe` does **not** revoke the pair on the relay or on the iPhone. The
user can re-pair afterwards by running `pair` again.

## Error codes

When `status == "failed"` or the CLI returns a non-zero exit code, the
JSON includes `{ "code": "...", "message": "..." }` (or nests them
under `error`). Common codes:

| code | meaning | what to do |
|---|---|---|
| `pair_incomplete` | Pairing has not finished. | Tell the user to run `healthbridge pair`. |
| `bad_auth` | Local pair record is stale (token rotated). | Tell the user to re-pair. |
| `scope_denied` | Requested type isn't in the granted scope set. | Either pick a different approach or ask the user to grant it via `scopes grant`. |
| `mailbox_full` | Relay queue is at capacity. | Wait and retry; surface the error to the user if it persists. |
| `anchor_invalidated` | The local sync cache is stale. | Run `healthbridge sync --full --type <t>` to recover. |
| `unsupported_type` | The requested sample type isn't wired in this CLI build. | Run `healthbridge types --json` to see what's supported. |
| `not_implemented` | The job kind is recognised but not yet implemented (e.g. workout writes). | Tell the user it's not yet supported. |
