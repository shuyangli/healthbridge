# Example: log a meal

User says: "I had a 500 kcal lunch — chicken and rice"

```sh
healthbridge write dietary_energy_consumed \
  --value 500 \
  --unit kcal \
  --at now \
  --meta description="chicken and rice" \
  --meta source=agent \
  --json
```

Expected JSON shape (success):

```json
{
  "job_id": "01JX...",
  "status": "done",
  "uuid": "<healthkit-uuid>"
}
```

If `status` is `pending`:

```json
{
  "job_id": "01JX...",
  "status": "pending"
}
```

Agent should remember the `job_id` and offer to check back later.
