# healthbridge — sample type catalog

The canonical list of HealthKit sample types this CLI supports, with
the unit string each one expects on writes. Generated from
`healthbridge types --json` against the binary at the time this skill
was packaged. **Run `healthbridge types --json` if you need the
authoritative list for the binary that's actually installed.**

## Activity

| type | unit | notes |
|---|---|---|
| `step_count` | `count` | Cumulative count over the sample interval. |
| `active_energy_burned` | `kcal` | Energy from active movement. |
| `basal_energy_burned` | `kcal` | Energy from resting metabolism. |

## Vitals

| type | unit | notes |
|---|---|---|
| `heart_rate` | `count/min` | Instantaneous HR. |
| `heart_rate_resting` | `count/min` | Daily resting HR aggregate. |
| `body_mass` | `kg` | Use kg even if the user gave you lbs — convert. |
| `body_fat_percentage` | `%` | 0–1 range or 0–100? HealthKit uses fraction; pass `0.18` for 18 %. |

## Nutrition — energy

| type | unit |
|---|---|
| `dietary_energy_consumed` | `kcal` |

## Nutrition — macronutrients

All macronutrients use grams or milligrams as below.

| type | unit |
|---|---|
| `dietary_protein` | `g` |
| `dietary_carbohydrates` | `g` |
| `dietary_fat_total` | `g` |
| `dietary_fat_saturated` | `g` |
| `dietary_fiber` | `g` |
| `dietary_sugar` | `g` |
| `dietary_cholesterol` | `mg` |
| `dietary_sodium` | `mg` |
| `dietary_caffeine` | `mg` |

## Hydration

| type | unit |
|---|---|
| `dietary_water` | `mL` |

## Sleep & workouts (read-only for now)

| type | unit | notes |
|---|---|---|
| `sleep_analysis` | `(category)` | Read via `healthbridge read sleep_analysis`; writes are not yet implemented. |
| `workout` | `(workout)` | Read/sync only; structured workout writes are M5+. |

## Picking the right type

- **"calories" without context** → `dietary_energy_consumed`. Don't
  guess between active vs basal — those are *expenditure* types written
  by Apple Watch, not by users.
- **Body weight** → always `body_mass`, never invent `weight`.
- **Heart rate** → use `heart_rate_resting` only when the user said
  "resting"; otherwise `heart_rate`.

## Logging a meal with macros

When the user gives you both calories and macros, write each as its own
sample with the same `--at` timestamp:

```sh
T="2026-04-07T12:30:00Z"
healthbridge write dietary_energy_consumed --value 620 --unit kcal --at "$T" --json
healthbridge write dietary_protein         --value 38  --unit g    --at "$T" --json
healthbridge write dietary_carbohydrates   --value 72  --unit g    --at "$T" --json
healthbridge write dietary_fat_total       --value 18  --unit g    --at "$T" --json
```

HealthKit will group samples written within the same minute under the
"Food" entry in the Health app.

## Unit gotchas

- HealthKit unit strings are case-sensitive. `kcal`, `kg`, `g`, `mg`,
  `mL` (capital L), `count`, `count/min`, `mg/dL`.
- Compound units use `/` (e.g. `count/min`, `mg/dL`).
- Percentages: HealthKit stores `body_fat_percentage` as a fraction
  (0.18 = 18 %). Convert before writing.
- Water: HealthKit prefers `mL`; if the user says "16 oz", convert to
  `473` mL.
