# healthbridge — sample type catalog

The canonical list of HealthKit sample types this CLI supports,
with the unit string each one expects on writes. **This file is
generated** from `cli/internal/health/catalog.go` by
`cli/cmd/gen-types`. Do not edit by hand. Run
`cd cli && go run ./cmd/gen-types` to regenerate after a catalog
change.

Run `healthbridge types --json` if you need the authoritative list
for the binary that's actually installed.

`write` = `yes` means the iOS app requests HealthKit write
authorization for that type at pairing time, so the agent can log
new samples for it. Read access is requested for every type below.

## Activity (34)

| type | unit | write |
|---|---|---|
| `active_energy_burned` | `kcal` | yes |
| `apple_exercise_time` | `min` |  |
| `apple_move_time` | `min` |  |
| `apple_stand_time` | `min` |  |
| `basal_energy_burned` | `kcal` |  |
| `cross_country_skiing_speed` | `m/s` |  |
| `cycling_cadence` | `count/min` |  |
| `cycling_functional_threshold_power` | `W` |  |
| `cycling_power` | `W` |  |
| `cycling_speed` | `m/s` |  |
| `distance_cross_country_skiing` | `m` |  |
| `distance_cycling` | `m` |  |
| `distance_downhill_snow_sports` | `m` |  |
| `distance_paddle_sports` | `m` |  |
| `distance_rowing` | `m` |  |
| `distance_skating_sports` | `m` |  |
| `distance_swimming` | `m` |  |
| `distance_walking_running` | `m` |  |
| `distance_wheelchair` | `m` |  |
| `estimated_workout_effort_score` | `count` |  |
| `flights_climbed` | `count` |  |
| `paddle_sports_speed` | `m/s` |  |
| `physical_effort` | `kcal/(kg*hr)` |  |
| `push_count` | `count` |  |
| `rowing_speed` | `m/s` |  |
| `running_ground_contact_time` | `ms` |  |
| `running_power` | `W` |  |
| `running_speed` | `m/s` |  |
| `running_stride_length` | `m` |  |
| `running_vertical_oscillation` | `cm` |  |
| `step_count` | `count` |  |
| `swimming_stroke_count` | `count` |  |
| `vo2_max` | `ml/(kg*min)` |  |
| `workout_effort_score` | `count` |  |

## Body measurements (7)

| type | unit | write |
|---|---|---|
| `apple_sleeping_wrist_temperature` | `degC` |  |
| `body_fat_percentage` | `%` | yes |
| `body_mass` | `kg` | yes |
| `body_mass_index` | `count` |  |
| `height` | `m` | yes |
| `lean_body_mass` | `kg` | yes |
| `waist_circumference` | `m` | yes |

## Vital signs (11)

| type | unit | write |
|---|---|---|
| `atrial_fibrillation_burden` | `%` |  |
| `blood_pressure_diastolic` | `mmHg` | yes |
| `blood_pressure_systolic` | `mmHg` | yes |
| `body_temperature` | `degC` |  |
| `heart_rate` | `count/min` |  |
| `heart_rate_recovery_one_minute` | `count/min` |  |
| `heart_rate_resting` | `count/min` |  |
| `heart_rate_variability_sdnn` | `ms` |  |
| `oxygen_saturation` | `%` |  |
| `respiratory_rate` | `count/min` |  |
| `walking_heart_rate_average` | `count/min` |  |

## Lab and test results (9)

| type | unit | write |
|---|---|---|
| `blood_glucose` | `mg/dL` | yes |
| `electrodermal_activity` | `S` |  |
| `forced_expiratory_volume_1` | `L` |  |
| `forced_vital_capacity` | `L` |  |
| `inhaler_usage` | `count` |  |
| `insulin_delivery` | `IU` |  |
| `number_of_times_fallen` | `count` |  |
| `peak_expiratory_flow_rate` | `L/min` |  |
| `peripheral_perfusion_index` | `%` |  |

## Nutrition (39)

| type | unit | write |
|---|---|---|
| `dietary_biotin` | `mcg` | yes |
| `dietary_caffeine` | `mg` | yes |
| `dietary_calcium` | `mg` | yes |
| `dietary_carbohydrates` | `g` | yes |
| `dietary_chloride` | `mg` | yes |
| `dietary_cholesterol` | `mg` | yes |
| `dietary_chromium` | `mcg` | yes |
| `dietary_copper` | `mg` | yes |
| `dietary_energy_consumed` | `kcal` | yes |
| `dietary_fat_monounsaturated` | `g` | yes |
| `dietary_fat_polyunsaturated` | `g` | yes |
| `dietary_fat_saturated` | `g` | yes |
| `dietary_fat_total` | `g` | yes |
| `dietary_fiber` | `g` | yes |
| `dietary_folate` | `mcg` | yes |
| `dietary_iodine` | `mcg` | yes |
| `dietary_iron` | `mg` | yes |
| `dietary_magnesium` | `mg` | yes |
| `dietary_manganese` | `mg` | yes |
| `dietary_molybdenum` | `mcg` | yes |
| `dietary_niacin` | `mg` | yes |
| `dietary_pantothenic_acid` | `mg` | yes |
| `dietary_phosphorus` | `mg` | yes |
| `dietary_potassium` | `mg` | yes |
| `dietary_protein` | `g` | yes |
| `dietary_riboflavin` | `mg` | yes |
| `dietary_selenium` | `mcg` | yes |
| `dietary_sodium` | `mg` | yes |
| `dietary_sugar` | `g` | yes |
| `dietary_thiamin` | `mg` | yes |
| `dietary_vitamin_a` | `mcg` | yes |
| `dietary_vitamin_b12` | `mcg` | yes |
| `dietary_vitamin_b6` | `mg` | yes |
| `dietary_vitamin_c` | `mg` | yes |
| `dietary_vitamin_d` | `mcg` | yes |
| `dietary_vitamin_e` | `mg` | yes |
| `dietary_vitamin_k` | `mcg` | yes |
| `dietary_water` | `mL` | yes |
| `dietary_zinc` | `mg` | yes |

## Hearing health (3)

| type | unit | write |
|---|---|---|
| `environmental_audio_exposure` | `dBASPL` |  |
| `environmental_sound_reduction` | `dBASPL` |  |
| `headphone_audio_exposure` | `dBASPL` |  |

## Mobility (8)

| type | unit | write |
|---|---|---|
| `apple_walking_steadiness` | `%` |  |
| `six_minute_walk_test_distance` | `m` |  |
| `stair_ascent_speed` | `m/s` |  |
| `stair_descent_speed` | `m/s` |  |
| `walking_asymmetry_percentage` | `%` |  |
| `walking_double_support_percentage` | `%` |  |
| `walking_speed` | `m/s` |  |
| `walking_step_length` | `cm` |  |

## Reproductive health (1)

| type | unit | write |
|---|---|---|
| `basal_body_temperature` | `degC` |  |

## UV exposure & daylight (2)

| type | unit | write |
|---|---|---|
| `time_in_daylight` | `min` |  |
| `uv_exposure` | `count` |  |

## Diving (2)

| type | unit | write |
|---|---|---|
| `underwater_depth` | `m` |  |
| `water_temperature` | `degC` |  |

## Alcohol (2)

| type | unit | write |
|---|---|---|
| `blood_alcohol_content` | `%` |  |
| `number_of_alcoholic_beverages` | `count` | yes |

## Sleep (extra quantity types) (1)

| type | unit | write |
|---|---|---|
| `apple_sleeping_breathing_disturbances` | `count` |  |

## Sleep & workouts (HKCategory / HKWorkout, read-only)

Both are reported as one `Sample` per HealthKit record, with
`value` set to the **duration in seconds** and `unit` set to `s`.
Categorical or activity-type information travels in `metadata`.

| type | unit | metadata fields |
|---|---|---|
| `sleep_analysis` | `s` | `state`: one of `in_bed`, `awake`, `asleep_unspecified`, `asleep_core`, `asleep_deep`, `asleep_rem` |
| `workout` | `s` | `activity_type` (e.g. `running`, `cycling`, `hiit`, …), and when present `total_energy_burned_kcal` and `total_distance_m` |

Writes are not yet implemented for either type.

## Picking the right type

- **"calories" without context** → `dietary_energy_consumed`. Don't
  guess between active vs basal — those are *expenditure* types written
  by Apple Watch, not by users.
- **Body weight** → always `body_mass`, never invent `weight`.
- **Heart rate** → use `heart_rate_resting` only when the user said
  "resting"; otherwise `heart_rate`.
- **Distance for a run/ride** → prefer the modality-specific type
  (`distance_walking_running`, `distance_cycling`, `distance_swimming`)
  over a generic distance count.

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
  `mcg`, `mL` (capital L), `count`, `count/min`, `mg/dL`.
- Compound units use `/` and parens: `count/min`, `mg/dL`,
  `ml/(kg*min)` for VO₂max.
- Percentages: HealthKit stores percentage-typed quantities (body
  fat, oxygen saturation, walking steadiness, AFib burden) as a
  fraction in `[0, 1]`. Convert before writing — pass `0.18` for
  18 %, not `18`.
- Water: HealthKit prefers `mL`; if the user says "16 oz", convert
  to `473` mL.
- Distances are metres. Convert miles/feet/yards before writing.
- Speeds are `m/s`. Convert km/h or mph before writing.
- Power is `W` (watts). HealthKit accepts `W` directly.
- Temperatures are degrees Celsius (`degC`). HealthKit will convert
  on read but `degC` is the canonical write unit.
