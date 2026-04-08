package health

// Catalog is the single source of truth for the HealthKit quantity types
// the CLI exposes. Every Go-side helper that takes a SampleType
// (AllSampleTypes, IsValid, canonicalUnitForType) is derived from this
// slice, and `cli/cmd/gen-types` regenerates proto/schema.json, the
// skill TYPES.md, and the iOS Catalog.swift from the same data.
//
// Adding a new HKQuantityTypeIdentifier means appending one Definition
// here and re-running `go run ./cmd/gen-types`. The Layer-1 invariant
// tests in catalog_test.go will fail loudly if the entry is malformed.
//
// Apple-deprecated identifiers (e.g. nikeFuel) are intentionally
// excluded.

// Category groups quantity types for human-readable output and for the
// HealthKit auth-sheet sectioning the iOS app does at pairing time.
type Category int

const (
	CategoryActivity Category = iota
	CategoryBodyMeasurement
	CategoryVitalSign
	CategoryLabResult
	CategoryNutrition
	CategoryHearing
	CategoryMobility
	CategoryReproductive
	CategoryUVExposure
	CategoryDiving
	CategoryAlcohol
	CategorySleep
)

// String returns a stable snake_case name for the category. Used by
// the codegen and by `healthbridge types` grouping.
func (c Category) String() string {
	switch c {
	case CategoryActivity:
		return "activity"
	case CategoryBodyMeasurement:
		return "body_measurement"
	case CategoryVitalSign:
		return "vital_sign"
	case CategoryLabResult:
		return "lab_result"
	case CategoryNutrition:
		return "nutrition"
	case CategoryHearing:
		return "hearing"
	case CategoryMobility:
		return "mobility"
	case CategoryReproductive:
		return "reproductive"
	case CategoryUVExposure:
		return "uv_exposure"
	case CategoryDiving:
		return "diving"
	case CategoryAlcohol:
		return "alcohol"
	case CategorySleep:
		return "sleep"
	default:
		return "unknown"
	}
}

// Aggregation tells the iOS app which HKStatisticsQuery option to use
// when summarising — cumulative types sum, discrete types average.
type Aggregation int

const (
	Cumulative Aggregation = iota
	Discrete
)

// Definition is one row of the catalog.
type Definition struct {
	// Wire is the snake_case name we use on the wire and in the CLI.
	// MUST stay stable for any type that has already shipped — pre-paired
	// CLIs persist these strings on disk.
	Wire SampleType
	// HKIdentifier is the lowerCamelCase HealthKit symbol name (e.g.
	// "runningPower"), without the "HKQuantityTypeIdentifier" prefix.
	// The Swift catalog reconstructs the full identifier as
	// HKQuantityTypeIdentifier(rawValue: "HKQuantityTypeIdentifier" + UpperCamel).
	HKIdentifier string
	Category     Category
	// Unit is the canonical wire-format unit string. The Swift unit
	// parser must accept this string and produce an HKUnit that is
	// compatible with this type's HKQuantityType.
	Unit        string
	Aggregation Aggregation
	// Writable indicates that the iOS app should request write
	// authorization for this type at pairing time. The CLI's `write`
	// subcommand still performs its own per-type scope check.
	Writable bool
}

// Catalog is the master list. Order is "by category, then existing
// types first, then new types alphabetically" so the diff for future
// additions is small. The Layer-1 test enforces uniqueness so order
// is purely cosmetic.
var Catalog = []Definition{
	// ----- Activity (34) -----
	{Wire: StepCount, HKIdentifier: "stepCount", Category: CategoryActivity, Unit: "count", Aggregation: Cumulative},
	{Wire: ActiveEnergyBurned, HKIdentifier: "activeEnergyBurned", Category: CategoryActivity, Unit: "kcal", Aggregation: Cumulative, Writable: true},
	{Wire: BasalEnergyBurned, HKIdentifier: "basalEnergyBurned", Category: CategoryActivity, Unit: "kcal", Aggregation: Cumulative},
	{Wire: DistanceWalkingRunning, HKIdentifier: "distanceWalkingRunning", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceCycling, HKIdentifier: "distanceCycling", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceSwimming, HKIdentifier: "distanceSwimming", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceWheelchair, HKIdentifier: "distanceWheelchair", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceDownhillSnowSports, HKIdentifier: "distanceDownhillSnowSports", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceCrossCountrySkiing, HKIdentifier: "distanceCrossCountrySkiing", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistancePaddleSports, HKIdentifier: "distancePaddleSports", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceRowing, HKIdentifier: "distanceRowing", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: DistanceSkatingSports, HKIdentifier: "distanceSkatingSports", Category: CategoryActivity, Unit: "m", Aggregation: Cumulative},
	{Wire: PushCount, HKIdentifier: "pushCount", Category: CategoryActivity, Unit: "count", Aggregation: Cumulative},
	{Wire: SwimmingStrokeCount, HKIdentifier: "swimmingStrokeCount", Category: CategoryActivity, Unit: "count", Aggregation: Cumulative},
	{Wire: FlightsClimbed, HKIdentifier: "flightsClimbed", Category: CategoryActivity, Unit: "count", Aggregation: Cumulative},
	{Wire: AppleExerciseTime, HKIdentifier: "appleExerciseTime", Category: CategoryActivity, Unit: "min", Aggregation: Cumulative},
	{Wire: AppleMoveTime, HKIdentifier: "appleMoveTime", Category: CategoryActivity, Unit: "min", Aggregation: Cumulative},
	{Wire: AppleStandTime, HKIdentifier: "appleStandTime", Category: CategoryActivity, Unit: "min", Aggregation: Cumulative},
	{Wire: VO2Max, HKIdentifier: "vo2Max", Category: CategoryActivity, Unit: "ml/(kg*min)", Aggregation: Discrete},
	{Wire: RunningPower, HKIdentifier: "runningPower", Category: CategoryActivity, Unit: "W", Aggregation: Discrete},
	{Wire: RunningSpeed, HKIdentifier: "runningSpeed", Category: CategoryActivity, Unit: "m/s", Aggregation: Discrete},
	{Wire: RunningStrideLength, HKIdentifier: "runningStrideLength", Category: CategoryActivity, Unit: "m", Aggregation: Discrete},
	{Wire: RunningVerticalOscillation, HKIdentifier: "runningVerticalOscillation", Category: CategoryActivity, Unit: "cm", Aggregation: Discrete},
	{Wire: RunningGroundContactTime, HKIdentifier: "runningGroundContactTime", Category: CategoryActivity, Unit: "ms", Aggregation: Discrete},
	{Wire: CyclingPower, HKIdentifier: "cyclingPower", Category: CategoryActivity, Unit: "W", Aggregation: Discrete},
	{Wire: CyclingSpeed, HKIdentifier: "cyclingSpeed", Category: CategoryActivity, Unit: "m/s", Aggregation: Discrete},
	{Wire: CyclingCadence, HKIdentifier: "cyclingCadence", Category: CategoryActivity, Unit: "count/min", Aggregation: Discrete},
	{Wire: CyclingFunctionalThresholdPower, HKIdentifier: "cyclingFunctionalThresholdPower", Category: CategoryActivity, Unit: "W", Aggregation: Discrete},
	{Wire: CrossCountrySkiingSpeed, HKIdentifier: "crossCountrySkiingSpeed", Category: CategoryActivity, Unit: "m/s", Aggregation: Discrete},
	{Wire: PaddleSportsSpeed, HKIdentifier: "paddleSportsSpeed", Category: CategoryActivity, Unit: "m/s", Aggregation: Discrete},
	{Wire: RowingSpeed, HKIdentifier: "rowingSpeed", Category: CategoryActivity, Unit: "m/s", Aggregation: Discrete},
	{Wire: PhysicalEffort, HKIdentifier: "physicalEffort", Category: CategoryActivity, Unit: "kcal/(kg*hr)", Aggregation: Discrete},
	// Effort scores use HKUnit.appleEffortScore() (custom unit added in
	// iOS 18). The HKUnit string `appleEffortScore` is recognised by
	// the iOS-side parser via an explicit case in HealthKitMapping.unit(from:).
	{Wire: WorkoutEffortScore, HKIdentifier: "workoutEffortScore", Category: CategoryActivity, Unit: "appleEffortScore", Aggregation: Discrete},
	{Wire: EstimatedWorkoutEffortScore, HKIdentifier: "estimatedWorkoutEffortScore", Category: CategoryActivity, Unit: "appleEffortScore", Aggregation: Discrete},

	// ----- Body measurements (7) -----
	{Wire: Height, HKIdentifier: "height", Category: CategoryBodyMeasurement, Unit: "m", Aggregation: Discrete, Writable: true},
	{Wire: BodyMass, HKIdentifier: "bodyMass", Category: CategoryBodyMeasurement, Unit: "kg", Aggregation: Discrete, Writable: true},
	{Wire: BodyMassIndex, HKIdentifier: "bodyMassIndex", Category: CategoryBodyMeasurement, Unit: "count", Aggregation: Discrete},
	{Wire: LeanBodyMass, HKIdentifier: "leanBodyMass", Category: CategoryBodyMeasurement, Unit: "kg", Aggregation: Discrete, Writable: true},
	{Wire: BodyFatPercentage, HKIdentifier: "bodyFatPercentage", Category: CategoryBodyMeasurement, Unit: "%", Aggregation: Discrete, Writable: true},
	{Wire: WaistCircumference, HKIdentifier: "waistCircumference", Category: CategoryBodyMeasurement, Unit: "m", Aggregation: Discrete, Writable: true},
	{Wire: AppleSleepingWristTemperature, HKIdentifier: "appleSleepingWristTemperature", Category: CategoryBodyMeasurement, Unit: "degC", Aggregation: Discrete},

	// ----- Vital signs (11) -----
	{Wire: HeartRate, HKIdentifier: "heartRate", Category: CategoryVitalSign, Unit: "count/min", Aggregation: Discrete},
	{Wire: HeartRateResting, HKIdentifier: "restingHeartRate", Category: CategoryVitalSign, Unit: "count/min", Aggregation: Discrete},
	{Wire: WalkingHeartRateAverage, HKIdentifier: "walkingHeartRateAverage", Category: CategoryVitalSign, Unit: "count/min", Aggregation: Discrete},
	{Wire: HeartRateVariabilitySDNN, HKIdentifier: "heartRateVariabilitySDNN", Category: CategoryVitalSign, Unit: "ms", Aggregation: Discrete},
	{Wire: HeartRateRecoveryOneMinute, HKIdentifier: "heartRateRecoveryOneMinute", Category: CategoryVitalSign, Unit: "count/min", Aggregation: Discrete},
	{Wire: AtrialFibrillationBurden, HKIdentifier: "atrialFibrillationBurden", Category: CategoryVitalSign, Unit: "%", Aggregation: Discrete},
	{Wire: OxygenSaturation, HKIdentifier: "oxygenSaturation", Category: CategoryVitalSign, Unit: "%", Aggregation: Discrete},
	{Wire: BodyTemperature, HKIdentifier: "bodyTemperature", Category: CategoryVitalSign, Unit: "degC", Aggregation: Discrete},
	{Wire: BloodPressureSystolic, HKIdentifier: "bloodPressureSystolic", Category: CategoryVitalSign, Unit: "mmHg", Aggregation: Discrete, Writable: true},
	{Wire: BloodPressureDiastolic, HKIdentifier: "bloodPressureDiastolic", Category: CategoryVitalSign, Unit: "mmHg", Aggregation: Discrete, Writable: true},
	{Wire: RespiratoryRate, HKIdentifier: "respiratoryRate", Category: CategoryVitalSign, Unit: "count/min", Aggregation: Discrete},

	// ----- Lab and test results (9) -----
	{Wire: BloodGlucose, HKIdentifier: "bloodGlucose", Category: CategoryLabResult, Unit: "mg/dL", Aggregation: Discrete, Writable: true},
	{Wire: ElectrodermalActivity, HKIdentifier: "electrodermalActivity", Category: CategoryLabResult, Unit: "S", Aggregation: Discrete},
	{Wire: ForcedExpiratoryVolume1, HKIdentifier: "forcedExpiratoryVolume1", Category: CategoryLabResult, Unit: "L", Aggregation: Discrete},
	{Wire: ForcedVitalCapacity, HKIdentifier: "forcedVitalCapacity", Category: CategoryLabResult, Unit: "L", Aggregation: Discrete},
	{Wire: InhalerUsage, HKIdentifier: "inhalerUsage", Category: CategoryLabResult, Unit: "count", Aggregation: Cumulative},
	{Wire: InsulinDelivery, HKIdentifier: "insulinDelivery", Category: CategoryLabResult, Unit: "IU", Aggregation: Cumulative},
	{Wire: NumberOfTimesFallen, HKIdentifier: "numberOfTimesFallen", Category: CategoryLabResult, Unit: "count", Aggregation: Cumulative},
	{Wire: PeakExpiratoryFlowRate, HKIdentifier: "peakExpiratoryFlowRate", Category: CategoryLabResult, Unit: "L/min", Aggregation: Discrete},
	{Wire: PeripheralPerfusionIndex, HKIdentifier: "peripheralPerfusionIndex", Category: CategoryLabResult, Unit: "%", Aggregation: Discrete},

	// ----- Nutrition (39) -----
	{Wire: DietaryEnergyConsumed, HKIdentifier: "dietaryEnergyConsumed", Category: CategoryNutrition, Unit: "kcal", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryWater, HKIdentifier: "dietaryWater", Category: CategoryNutrition, Unit: "mL", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryProtein, HKIdentifier: "dietaryProtein", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryCarbohydrates, HKIdentifier: "dietaryCarbohydrates", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFiber, HKIdentifier: "dietaryFiber", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietarySugar, HKIdentifier: "dietarySugar", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFatTotal, HKIdentifier: "dietaryFatTotal", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFatSaturated, HKIdentifier: "dietaryFatSaturated", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFatMonounsaturated, HKIdentifier: "dietaryFatMonounsaturated", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFatPolyunsaturated, HKIdentifier: "dietaryFatPolyunsaturated", Category: CategoryNutrition, Unit: "g", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryCholesterol, HKIdentifier: "dietaryCholesterol", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietarySodium, HKIdentifier: "dietarySodium", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryPotassium, HKIdentifier: "dietaryPotassium", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryCalcium, HKIdentifier: "dietaryCalcium", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryIron, HKIdentifier: "dietaryIron", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryMagnesium, HKIdentifier: "dietaryMagnesium", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryPhosphorus, HKIdentifier: "dietaryPhosphorus", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryZinc, HKIdentifier: "dietaryZinc", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryCopper, HKIdentifier: "dietaryCopper", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryManganese, HKIdentifier: "dietaryManganese", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryChromium, HKIdentifier: "dietaryChromium", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryIodine, HKIdentifier: "dietaryIodine", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryMolybdenum, HKIdentifier: "dietaryMolybdenum", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietarySelenium, HKIdentifier: "dietarySelenium", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryChloride, HKIdentifier: "dietaryChloride", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryCaffeine, HKIdentifier: "dietaryCaffeine", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminA, HKIdentifier: "dietaryVitaminA", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminB6, HKIdentifier: "dietaryVitaminB6", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminB12, HKIdentifier: "dietaryVitaminB12", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminC, HKIdentifier: "dietaryVitaminC", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminD, HKIdentifier: "dietaryVitaminD", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminE, HKIdentifier: "dietaryVitaminE", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryVitaminK, HKIdentifier: "dietaryVitaminK", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryThiamin, HKIdentifier: "dietaryThiamin", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryRiboflavin, HKIdentifier: "dietaryRiboflavin", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryNiacin, HKIdentifier: "dietaryNiacin", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryFolate, HKIdentifier: "dietaryFolate", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryPantothenicAcid, HKIdentifier: "dietaryPantothenicAcid", Category: CategoryNutrition, Unit: "mg", Aggregation: Cumulative, Writable: true},
	{Wire: DietaryBiotin, HKIdentifier: "dietaryBiotin", Category: CategoryNutrition, Unit: "mcg", Aggregation: Cumulative, Writable: true},

	// ----- Hearing health (3) -----
	{Wire: EnvironmentalAudioExposure, HKIdentifier: "environmentalAudioExposure", Category: CategoryHearing, Unit: "dBASPL", Aggregation: Discrete},
	{Wire: EnvironmentalSoundReduction, HKIdentifier: "environmentalSoundReduction", Category: CategoryHearing, Unit: "dBASPL", Aggregation: Discrete},
	{Wire: HeadphoneAudioExposure, HKIdentifier: "headphoneAudioExposure", Category: CategoryHearing, Unit: "dBASPL", Aggregation: Discrete},

	// ----- Mobility (8) -----
	{Wire: AppleWalkingSteadiness, HKIdentifier: "appleWalkingSteadiness", Category: CategoryMobility, Unit: "%", Aggregation: Discrete},
	{Wire: WalkingSpeed, HKIdentifier: "walkingSpeed", Category: CategoryMobility, Unit: "m/s", Aggregation: Discrete},
	{Wire: WalkingStepLength, HKIdentifier: "walkingStepLength", Category: CategoryMobility, Unit: "cm", Aggregation: Discrete},
	{Wire: WalkingAsymmetryPercentage, HKIdentifier: "walkingAsymmetryPercentage", Category: CategoryMobility, Unit: "%", Aggregation: Discrete},
	{Wire: WalkingDoubleSupportPercentage, HKIdentifier: "walkingDoubleSupportPercentage", Category: CategoryMobility, Unit: "%", Aggregation: Discrete},
	{Wire: StairAscentSpeed, HKIdentifier: "stairAscentSpeed", Category: CategoryMobility, Unit: "m/s", Aggregation: Discrete},
	{Wire: StairDescentSpeed, HKIdentifier: "stairDescentSpeed", Category: CategoryMobility, Unit: "m/s", Aggregation: Discrete},
	{Wire: SixMinuteWalkTestDistance, HKIdentifier: "sixMinuteWalkTestDistance", Category: CategoryMobility, Unit: "m", Aggregation: Discrete},

	// ----- Reproductive health (1 quantity) -----
	{Wire: BasalBodyTemperature, HKIdentifier: "basalBodyTemperature", Category: CategoryReproductive, Unit: "degC", Aggregation: Discrete},

	// ----- UV / daylight (2) -----
	{Wire: UVExposure, HKIdentifier: "uvExposure", Category: CategoryUVExposure, Unit: "count", Aggregation: Discrete},
	{Wire: TimeInDaylight, HKIdentifier: "timeInDaylight", Category: CategoryUVExposure, Unit: "min", Aggregation: Cumulative},

	// ----- Diving (2) -----
	{Wire: UnderwaterDepth, HKIdentifier: "underwaterDepth", Category: CategoryDiving, Unit: "m", Aggregation: Discrete},
	{Wire: WaterTemperature, HKIdentifier: "waterTemperature", Category: CategoryDiving, Unit: "degC", Aggregation: Discrete},

	// ----- Alcohol (2) -----
	{Wire: BloodAlcoholContent, HKIdentifier: "bloodAlcoholContent", Category: CategoryAlcohol, Unit: "%", Aggregation: Discrete},
	{Wire: NumberOfAlcoholicBeverages, HKIdentifier: "numberOfAlcoholicBeverages", Category: CategoryAlcohol, Unit: "count", Aggregation: Cumulative, Writable: true},

	// ----- Sleep (extra quantity, iOS 18+) (1) -----
	{Wire: AppleSleepingBreathingDisturbances, HKIdentifier: "appleSleepingBreathingDisturbances", Category: CategorySleep, Unit: "count", Aggregation: Cumulative},
}

// catalogByWire is a one-time index built at package init for O(1)
// lookups by wire name.
var catalogByWire = func() map[SampleType]*Definition {
	m := make(map[SampleType]*Definition, len(Catalog))
	for i := range Catalog {
		d := &Catalog[i]
		m[d.Wire] = d
	}
	return m
}()

// LookupByWire returns the catalog entry for a wire-format SampleType,
// or nil if t is not a quantity type the catalog knows about. Returns
// nil for sleep_analysis and workout (which are not quantity types).
func LookupByWire(t SampleType) *Definition {
	return catalogByWire[t]
}

// CatalogWireNames returns the wire names of every catalog entry, in
// catalog order. Used by the codegen and by AllSampleTypes.
func CatalogWireNames() []SampleType {
	out := make([]SampleType, len(Catalog))
	for i := range Catalog {
		out[i] = Catalog[i].Wire
	}
	return out
}
