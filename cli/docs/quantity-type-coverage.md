# Full HKQuantityTypeIdentifier coverage

- Owner: shuyangli
- Last updated: 2026-04-07
- Current status: planning. Catalog drafted from
  `developer.apple.com/tutorials/data/documentation/healthkit/hkquantitytypeidentifier.json`;
  no code written yet. Awaiting user sign-off on the approach below
  before starting milestone M1.

## Motivation

Today the CLI exposes 22 of the ~110 `HKQuantityTypeIdentifier`
constants Apple defines (plus `sleep_analysis` and `workout`). For an
agent that wants to give the user any meaningful summary of their Apple
Health data — running power, walking steadiness, blood pressure, VO₂
max, micronutrients, audio exposure — the curated subset is the wrong
default. Each missing type forces a code change in five files, which is
why coverage has stalled.

The goal of this work is to:

1. Expose **every** quantity type Apple ships, so the agent can ask
   `healthbridge read <anything>` without a CLI release in between.
2. Replace the five hand-maintained switch statements with a single
   catalog so future additions are one-line edits.
3. Add a thorough test layer for the parsing surface (wire enum,
   canonical units, HKUnit construction, JSON round-trip) so the
   ~5× growth in surface area doesn't translate into ~5× regressions.

Out of scope: category types beyond `sleep_analysis`, characteristic
types (DOB / biological sex), clinical / FHIR records, and the
`HKWorkoutType`. Those need their own value model and aren't part of
this milestone.

## Design overview

### Single source of truth

A new file `cli/internal/health/catalog.go` defines:

```go
type Definition struct {
    Wire         SampleType   // "running_power"
    HKIdentifier string       // "HKQuantityTypeIdentifierRunningPower"
    Category     Category     // Activity, BodyMeasurements, ...
    Unit         string       // canonical wire-format unit string
    Aggregation  Aggregation  // cumulative | discrete
    Writable     bool         // true if we'll request write authorization
    Notes        string       // optional one-liner for `types` output
}

var Catalog = []Definition{ ... ~110 entries ... }
```

Everything else in the Go layer is derived from `Catalog`:

- `AllSampleTypes()` → `for _, d := range Catalog { ... d.Wire }`
- `IsValid()` → map lookup built once
- `canonicalUnitForType` → map lookup
- `SampleType` constants → still listed explicitly so existing call
  sites compile, but the constant *list* and the catalog are kept in
  sync by a test that fails if either is missing an entry the other
  has.

### Schema generation

`proto/schema.json` and `skill/healthbridge/references/TYPES.md` are
both regenerated from the catalog by a small `go run
./cmd/gen-types` script (added under `cli/cmd/gen-types/`). The
generator runs in CI in check mode (`-check`) and fails the build if
the committed file doesn't match. Same pattern as `go generate`-style
codegen, but explicit.

### Swift side

The current Swift `SampleType` enum has one `case` per wire type and
`HealthKitMapping.swift` has a 22-arm switch. Both scale poorly to
~110 entries. We replace them with:

1. **`SampleType` becomes a `RawRepresentable` struct**:

   ```swift
   public struct SampleType: RawRepresentable, Codable, Hashable, Sendable {
       public let rawValue: String
       public init(rawValue: String) { self.rawValue = rawValue }
   }
   ```

   Existing call sites that use `SampleType.stepCount` continue to
   work because we keep static constants on the type:
   `public static let stepCount = SampleType(rawValue: "step_count")`.
   The constants are codegen'd from the same catalog (a tiny
   `gen-types` Swift output target) so they cannot drift.

2. **`HealthKitMapping.quantityIdentifier(for:)` becomes a dictionary
   lookup** seeded from a generated `[String: HKQuantityTypeIdentifier]`
   table. The same generator emits the canonical-unit table so the
   Swift `canonicalUnit(for:)` switch goes away.

3. **Read/write scope sets** are derived from a single
   `Catalog.iter().filter { $0.writable }`-equivalent on the Swift
   side, instead of the hand-maintained `writable` array in
   `HealthKitMapping.writeScopes()`.

The Swift catalog file (`ios/Sources/HealthBridgeKit/Generated/Catalog.swift`)
is checked in, not generated at build time, so the iOS app build stays
plain SwiftPM.

### Unit parser

The current `HealthKitMapping.unit(from:)` accepts ~16 unit strings.
With the full quantity-type set we need at minimum:

| Category | Units we'll add |
|---|---|
| Distance | (already: m, cm, in) — add `km`, `mi`, `ft`, `yd` |
| Speed | `m/s`, `km/h`, `mi/h` |
| Power | `W`, `kW` |
| Pressure | `mmHg`, `cmH2O`, `inHg` |
| Sound | `dBASPL`, `dBHL` |
| Temperature | `degC`, `degF`, `K` |
| Volume | (already: mL, L) — add `fl_oz_imp` |
| VO₂ | `mL/kg·min` (HKUnit string `ml/(kg*min)`) |
| Glucose | (already: mg/dL, mmol/L) |
| Insulin | `IU` |
| Conductance | `S` (siemens), `mS`, `µS` |
| Frequency | `Hz` |

For each unit string we accept, the canonical wire form is exactly
what Apple's `HKUnit(from:)` would parse. We **never** invent unit
strings; if Apple uses `ml/(kg*min)` we use the same characters. This
keeps the round-trip sample → wire → sample lossless.

## The catalog (draft)

The full table is below. Where Apple's API name maps mechanically to
snake_case (e.g. `runningPower` → `running_power`) we use that. Where
the existing wire name diverges (e.g. `restingHeartRate` →
`heart_rate_resting`) we keep the existing wire name to avoid breaking
shipped pair records, and document the divergence in a "wire" column.

Legend: **W** = we request write scope, **C** = cumulative, **D** =
discrete (HealthKit aggregation style).

### Activity (35)

| wire | HKIdentifier | unit | aggr | W |
|---|---|---|---|---|
| `step_count` | stepCount | count | C | |
| `distance_walking_running` | distanceWalkingRunning | m | C | |
| `distance_cycling` | distanceCycling | m | C | |
| `distance_swimming` | distanceSwimming | m | C | |
| `distance_wheelchair` | distanceWheelchair | m | C | |
| `distance_downhill_snow_sports` | distanceDownhillSnowSports | m | C | |
| `distance_cross_country_skiing` | distanceCrossCountrySkiing | m | C | |
| `distance_paddle_sports` | distancePaddleSports | m | C | |
| `distance_rowing` | distanceRowing | m | C | |
| `distance_skating_sports` | distanceSkatingSports | m | C | |
| `push_count` | pushCount | count | C | |
| `swimming_stroke_count` | swimmingStrokeCount | count | C | |
| `flights_climbed` | flightsClimbed | count | C | |
| `nike_fuel` | nikeFuel | count | C | |
| `apple_exercise_time` | appleExerciseTime | min | C | |
| `apple_move_time` | appleMoveTime | min | C | |
| `apple_stand_time` | appleStandTime | min | C | |
| `active_energy_burned` | activeEnergyBurned | kcal | C | W |
| `basal_energy_burned` | basalEnergyBurned | kcal | C | |
| `vo2_max` | vo2Max | ml/(kg*min) | D | |
| `running_power` | runningPower | W | D | |
| `running_speed` | runningSpeed | m/s | D | |
| `running_stride_length` | runningStrideLength | m | D | |
| `running_vertical_oscillation` | runningVerticalOscillation | cm | D | |
| `running_ground_contact_time` | runningGroundContactTime | ms | D | |
| `cycling_power` | cyclingPower | W | D | |
| `cycling_speed` | cyclingSpeed | m/s | D | |
| `cycling_cadence` | cyclingCadence | count/min | D | |
| `cycling_functional_threshold_power` | cyclingFunctionalThresholdPower | W | D | |
| `cross_country_skiing_speed` | crossCountrySkiingSpeed | m/s | D | |
| `paddle_sports_speed` | paddleSportsSpeed | m/s | D | |
| `rowing_speed` | rowingSpeed | m/s | D | |
| `physical_effort` | physicalEffort | kcal/(kg*hr) | D | |
| `workout_effort_score` | workoutEffortScore | count | D | |
| `estimated_workout_effort_score` | estimatedWorkoutEffortScore | count | D | |

### Body measurements (7)

| wire | HKIdentifier | unit | aggr | W |
|---|---|---|---|---|
| `height` | height | m | D | W |
| `body_mass` | bodyMass | kg | D | W |
| `body_mass_index` | bodyMassIndex | count | D | |
| `lean_body_mass` | leanBodyMass | kg | D | W |
| `body_fat_percentage` | bodyFatPercentage | % | D | W |
| `waist_circumference` | waistCircumference | m | D | W |
| `apple_sleeping_wrist_temperature` | appleSleepingWristTemperature | degC | D | |

### Vital signs (11)

| wire | HKIdentifier | unit | aggr | W |
|---|---|---|---|---|
| `heart_rate` | heartRate | count/min | D | |
| `heart_rate_resting` | restingHeartRate | count/min | D | |
| `walking_heart_rate_average` | walkingHeartRateAverage | count/min | D | |
| `heart_rate_variability_sdnn` | heartRateVariabilitySDNN | ms | D | |
| `heart_rate_recovery_one_minute` | heartRateRecoveryOneMinute | count/min | D | |
| `atrial_fibrillation_burden` | atrialFibrillationBurden | % | D | |
| `oxygen_saturation` | oxygenSaturation | % | D | |
| `body_temperature` | bodyTemperature | degC | D | |
| `blood_pressure_systolic` | bloodPressureSystolic | mmHg | D | |
| `blood_pressure_diastolic` | bloodPressureDiastolic | mmHg | D | |
| `respiratory_rate` | respiratoryRate | count/min | D | |

### Lab and test results (9)

| wire | HKIdentifier | unit | aggr | W |
|---|---|---|---|---|
| `blood_glucose` | bloodGlucose | mg/dL | D | |
| `electrodermal_activity` | electrodermalActivity | S | D | |
| `forced_expiratory_volume_1` | forcedExpiratoryVolume1 | L | D | |
| `forced_vital_capacity` | forcedVitalCapacity | L | D | |
| `inhaler_usage` | inhalerUsage | count | C | |
| `insulin_delivery` | insulinDelivery | IU | C | |
| `number_of_times_fallen` | numberOfTimesFallen | count | C | |
| `peak_expiratory_flow_rate` | peakExpiratoryFlowRate | L/min | D | |
| `peripheral_perfusion_index` | peripheralPerfusionIndex | % | D | |

### Nutrition (38)

All macronutrients are in `g`, minerals in `mg` or `µg`, vitamins
mostly `mg` / `µg`, energy in `kcal`, water in `mL`. Existing wire
names (`dietary_protein`, etc.) are preserved.

| wire | HKIdentifier | unit | W |
|---|---|---|---|
| `dietary_energy_consumed` | dietaryEnergyConsumed | kcal | W |
| `dietary_water` | dietaryWater | mL | W |
| `dietary_protein` | dietaryProtein | g | W |
| `dietary_carbohydrates` | dietaryCarbohydrates | g | W |
| `dietary_fiber` | dietaryFiber | g | W |
| `dietary_sugar` | dietarySugar | g | W |
| `dietary_fat_total` | dietaryFatTotal | g | W |
| `dietary_fat_saturated` | dietaryFatSaturated | g | W |
| `dietary_fat_monounsaturated` | dietaryFatMonounsaturated | g | W |
| `dietary_fat_polyunsaturated` | dietaryFatPolyunsaturated | g | W |
| `dietary_cholesterol` | dietaryCholesterol | mg | W |
| `dietary_sodium` | dietarySodium | mg | W |
| `dietary_potassium` | dietaryPotassium | mg | W |
| `dietary_calcium` | dietaryCalcium | mg | W |
| `dietary_iron` | dietaryIron | mg | W |
| `dietary_magnesium` | dietaryMagnesium | mg | W |
| `dietary_phosphorus` | dietaryPhosphorus | mg | W |
| `dietary_zinc` | dietaryZinc | mg | W |
| `dietary_copper` | dietaryCopper | mg | W |
| `dietary_manganese` | dietaryManganese | mg | W |
| `dietary_chromium` | dietaryChromium | µg | W |
| `dietary_iodine` | dietaryIodine | µg | W |
| `dietary_molybdenum` | dietaryMolybdenum | µg | W |
| `dietary_selenium` | dietarySelenium | µg | W |
| `dietary_chloride` | dietaryChloride | mg | W |
| `dietary_caffeine` | dietaryCaffeine | mg | W |
| `dietary_vitamin_a` | dietaryVitaminA | µg | W |
| `dietary_vitamin_b6` | dietaryVitaminB6 | mg | W |
| `dietary_vitamin_b12` | dietaryVitaminB12 | µg | W |
| `dietary_vitamin_c` | dietaryVitaminC | mg | W |
| `dietary_vitamin_d` | dietaryVitaminD | µg | W |
| `dietary_vitamin_e` | dietaryVitaminE | mg | W |
| `dietary_vitamin_k` | dietaryVitaminK | µg | W |
| `dietary_thiamin` | dietaryThiamin | mg | W |
| `dietary_riboflavin` | dietaryRiboflavin | mg | W |
| `dietary_niacin` | dietaryNiacin | mg | W |
| `dietary_folate` | dietaryFolate | µg | W |
| `dietary_pantothenic_acid` | dietaryPantothenicAcid | mg | W |
| `dietary_biotin` | dietaryBiotin | µg | W |

### Hearing health (3)

| wire | HKIdentifier | unit | aggr |
|---|---|---|---|
| `environmental_audio_exposure` | environmentalAudioExposure | dBASPL | D |
| `environmental_sound_reduction` | environmentalSoundReduction | dBASPL | D |
| `headphone_audio_exposure` | headphoneAudioExposure | dBASPL | D |

### Mobility (8)

| wire | HKIdentifier | unit | aggr |
|---|---|---|---|
| `apple_walking_steadiness` | appleWalkingSteadiness | % | D |
| `walking_speed` | walkingSpeed | m/s | D |
| `walking_step_length` | walkingStepLength | cm | D |
| `walking_asymmetry_percentage` | walkingAsymmetryPercentage | % | D |
| `walking_double_support_percentage` | walkingDoubleSupportPercentage | % | D |
| `stair_ascent_speed` | stairAscentSpeed | m/s | D |
| `stair_descent_speed` | stairDescentSpeed | m/s | D |
| `six_minute_walk_test_distance` | sixMinuteWalkTestDistance | m | D |

### Reproductive health (1 quantity)

| wire | HKIdentifier | unit | aggr |
|---|---|---|---|
| `basal_body_temperature` | basalBodyTemperature | degC | D |

### UV exposure / daylight (2)

| wire | HKIdentifier | unit | aggr | W |
|---|---|---|---|---|
| `uv_exposure` | uvExposure | count | D | |
| `time_in_daylight` | timeInDaylight | min | C | |

### Diving (2)

| wire | HKIdentifier | unit | aggr |
|---|---|---|---|
| `underwater_depth` | underwaterDepth | m | D |
| `water_temperature` | waterTemperature | degC | D |

### Alcohol (2)

| wire | HKIdentifier | unit | W |
|---|---|---|---|
| `blood_alcohol_content` | bloodAlcoholContent | % | |
| `number_of_alcoholic_beverages` | numberOfAlcoholicBeverages | count | W |

### Sleep (1 extra quantity, iOS 18+)

| wire | HKIdentifier | unit |
|---|---|---|
| `apple_sleeping_breathing_disturbances` | appleSleepingBreathingDisturbances | count |

### Carryover (existing non-quantity, untouched by this milestone)

- `sleep_analysis` (HKCategoryType)
- `workout` (HKWorkoutType)

### Total

≈ 119 quantity types + 2 carryover non-quantity = **121 entries** in
the catalog. (Apple's count varies by iOS version; we target the iOS
18 SDK that the app already builds against.)

## Risks and mitigation

| risk | mitigation |
|---|---|
| Wire-name churn breaks pre-shipped pair records (e.g. someone has `heart_rate_resting` saved). | Preserve every existing wire name verbatim. The catalog has a `Wire` column distinct from the auto-derived snake_case so divergences are explicit and tested. |
| Wrong canonical unit silently corrupts user data (e.g. emitting `kg` for a type Apple stores in `g`). | (1) `TestCanonicalUnitParses` constructs `HKUnit(from:)` for every catalog entry on the iOS side and asserts the result is `.isCompatible(with:)` with HealthKit's preferred unit for that identifier. (2) `TestCatalogUnitsMatchHKUnit` round-trips a 1.0 sample value through wire → HKUnit → wire and asserts equality. |
| Some `HKQuantityTypeIdentifier`s don't exist on older iOS targets (e.g. `appleSleepingBreathingDisturbances` is iOS 18+). | The Swift catalog wraps each entry's identifier construction in `HKObjectType.quantityType(forIdentifier:)`, which returns nil for unknown identifiers. The drain loop logs and skips unknown types instead of crashing. iOS-side test asserts ≥ 95 % of catalog entries resolve on the simulator's current SDK. |
| 5× growth in `readScopes()` means a much larger HealthKit auth sheet at pairing. | This is intentional — that's the cost of "agent can read anything". Documented in PRIVACY.md. We do **not** widen `writeScopes()` beyond the curated list; sensors and clinical data stay read-only. |
| Catalog drift between Go and Swift. | Single `gen-types` script generates both. CI runs it in `-check` mode. Test asserts catalog cardinality matches across layers. |
| 119-entry switch statements blow up compile time. | Replaced by map lookups, not switches. Compile-time impact is one slice literal in Go and one dictionary literal in Swift. |
| Some types (e.g. `nikeFuel`) are deprecated by Apple. | Include them with a `Deprecated bool` flag; `healthbridge types` hides them by default, `--all` shows them. |

## Test plan

This is the part the user explicitly called out — the parsing surface
grows ~5× and we want regressions caught locally, not at the user's
HealthKit drain time.

### Layer 1 — Go catalog invariants (`cli/internal/health/catalog_test.go`)

- `TestCatalogNoDuplicateWireNames` — every `Wire` is unique.
- `TestCatalogNoDuplicateHKIdentifiers` — every `HKIdentifier` is unique.
- `TestCatalogWireNamesAreSnakeCase` — regex `^[a-z][a-z0-9_]*$`.
- `TestCatalogHKIdentifiersAreLowerCamel` — regex `^[a-z][A-Za-z0-9]*$`.
- `TestCatalogHasUnit` — `Unit != ""` for every entry.
- `TestCatalogMatchesAllSampleTypes` — `len(Catalog) == len(AllSampleTypes())` and the sets are equal.
- `TestCatalogPreservedExistingNames` — table of the 22 pre-existing
  wire names hard-coded; assert each still resolves to the expected
  HKIdentifier. Guards against wire-format breakage.

### Layer 2 — Wire round-trip (`cli/internal/health/wire_test.go`)

For every entry in the catalog:

- Marshal a `Sample{Type: d.Wire, Value: 1.5, Unit: d.Unit, Start, End}`
  to JSON.
- Unmarshal it back.
- Assert deep equality.

This catches typos in struct tags, missing JSON fields, and any unit
string that contains characters JSON refuses to encode.

### Layer 3 — Schema generator round-trip (`cli/cmd/gen-types/gen_test.go`)

- `TestGenerateSchemaIsStable` — run the generator into a temp dir,
  diff against checked-in `proto/schema.json`, fail with a clear
  "run `go run ./cmd/gen-types` to update" message.
- `TestGenerateTypesMD` — same for `skill/healthbridge/references/TYPES.md`.
- `TestGenerateSwiftCatalog` — same for the generated Swift file.

### Layer 4 — CLI surface (`cli/cmd/healthbridge/cmd/types_test.go`)

Extend the existing tests to assert:

- `healthbridge types --json` returns ≥ 100 entries.
- Every entry has a non-empty `unit`.
- A spot-check of one type per category (e.g. `running_power`,
  `walking_steadiness`, `dietary_vitamin_d`, `environmental_audio_exposure`,
  `underwater_depth`) returns the expected unit.

### Layer 5 — Read job parsing (`cli/cmd/healthbridge/cmd/read_test.go`)

- Table-driven test: for each catalog entry, run
  `healthbridge read <type> --from -1d --json` against the fakerelay,
  decode the response, assert it round-trips. Confirms the read
  command's argument parser accepts every wire name (this is where
  most user-facing breakage would surface today).

### Layer 6 — Swift catalog and HKUnit construction (`ios/Tests/HealthBridgeKitTests/CatalogTests.swift`)

- `testCatalogCardinalityMatchesGo` — load `proto/schema.json` (which
  is shared) at test time, parse the `sampleType.enum` array, assert
  it equals `Catalog.allCases.map { $0.wire.rawValue }`.
- `testEveryUnitParsesAsHKUnit` — for each catalog entry, call
  `HealthKitMapping.unit(from: d.unit)`, assert non-nil and that
  `HKUnit(from: d.unit)` would not throw. (Run on the iOS XCTest
  target; HealthKit is unavailable on macOS unit tests.)
- `testEveryHKIdentifierResolvesOnDevice` — for each catalog entry,
  `HKObjectType.quantityType(forIdentifier:)` is non-nil. Skipped
  entries are recorded so we can document iOS-version gaps.
- `testCanonicalUnitIsCompatibleWithIdentifier` — for each entry,
  build the HKQuantityType and assert
  `HKQuantitySample(type:, quantity: HKQuantity(unit: parsed, doubleValue: 1.0), start:, end:)`
  succeeds without throwing. This is the **single most important test**
  in the whole plan — it's what catches "we said `kg` but Apple wants
  `count`" before any user sees it.

### Layer 7 — Read scenario tests (`cli/cmd/healthbridge/cmd/scenario_test.go`)

- Add a fakerelay scenario that, for each of ~10 representative new
  types, drains a synthetic read result and asserts the sample comes
  back through the CLI's JSON output verbatim. Reuses the existing
  scenario harness; about a 60-line diff.

### Layer 8 — Skill catalog generator output

- The regenerated `skill/healthbridge/references/TYPES.md` is
  diffed in CI. The skill manifest depends on this file, so any
  drift between the binary and the doc breaks the agent's grounding.

## Milestones

Each milestone is a self-contained PR that leaves `main` green.

### M1 — Go catalog + generated `AllSampleTypes`

- Add `cli/internal/health/catalog.go` with the full table from this
  doc.
- Re-derive `AllSampleTypes()`, `IsValid()`, and the Go-side
  `canonicalUnitForType` from the catalog.
- Keep the existing exported `SampleType` constants unchanged so no
  call site breaks.
- Add layer-1 and layer-2 tests above.
- **Validation:** `go test ./...` race-clean; `healthbridge types`
  prints the full list; `healthbridge read running_power --from -1d`
  produces a job that parses (even if no iOS app is paired).

### M2 — `gen-types` script + checked-in regenerated artifacts

- New `cli/cmd/gen-types/main.go` that emits:
  - `proto/schema.json` (regenerates the `sampleType.enum` block)
  - `skill/healthbridge/references/TYPES.md`
  - `ios/Sources/HealthBridgeKit/Generated/Catalog.swift`
- Add layer-3 tests (generator stability checks).
- Commit the generated files.
- **Validation:** generator is idempotent (running twice produces no
  diff); CI passes.

### M3 — Swift catalog + `SampleType` struct migration

- Replace the `SampleType` enum with the `RawRepresentable` struct.
- Wire `HealthKitMapping.quantityIdentifier(for:)` and
  `canonicalUnit(for:)` through the generated catalog dictionary.
- Update `readScopes()` / `writeScopes()` to derive from the catalog.
- Extend `HealthKitMapping.unit(from:)` to cover every unit string in
  the catalog (Layer 6 assertions force completeness).
- Add layer-6 tests on the iOS XCTest target.
- **Validation:** XCTest suite passes on a real iOS simulator; the
  HealthBridge app builds with no warnings; pairing on the simulator
  shows the expanded read sheet.

### M4 — End-to-end coverage tests + skill doc refresh

- Add layer-4, layer-5, layer-7 tests in Go.
- Run `go run ./cmd/gen-types` to refresh `TYPES.md` and friends.
- Update `cli/README.md` and `skill/healthbridge/SKILL.md` to mention
  the expanded type set.
- **Validation:** full test suite (`go test ./...`,
  `xcodebuild test`) green; manual smoke test:
  `healthbridge read environmental_audio_exposure --from -1d --json`
  on a real iPhone returns the expected samples.

## Open questions for the user

1. **Coverage cutoff** — keep 100 % of Apple's quantity types (incl.
   deprecated `nikeFuel`), or skip the deprecated ones entirely?
2. **Write scope expansion** — the draft only widens write scope to
   the macronutrient/mineral/vitamin column. Do you want write scope
   on body measurements like `waist_circumference`, or keep the
   current "agent can log meals + body weight + body fat" surface?
3. **Codegen vs. handwritten Swift catalog** — the plan above checks
   in a generated Swift file. Alternative: write the Swift catalog by
   hand once and rely on the layer-6 tests to keep it in sync. The
   handwritten path skips the codegen complexity but loses the
   "single source of truth" property.
4. **Category / characteristic types** — out of scope per this doc.
   Confirm or push back?
