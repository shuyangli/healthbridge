# Example: weekly activity summary

User says: "How was my walking this week?"

```sh
healthbridge read step_count --from -7d --to now --json
```

Expected JSON shape:

```json
{
  "job_id": "01JX...",
  "status": "done",
  "type": "step_count",
  "samples": [
    {
      "type": "step_count",
      "value": 8421,
      "unit": "count",
      "start": "2026-04-06T00:00:00Z",
      "end": "2026-04-07T00:00:00Z",
      "uuid": "..."
    }
  ]
}
```

The agent then groups the samples by day and emits a human summary
("you averaged 8200 steps; your best day was Wednesday with 11k").

For larger backfills, prefer `healthbridge sync --type step_count`
which uses an anchored delta query and only pulls new samples.
