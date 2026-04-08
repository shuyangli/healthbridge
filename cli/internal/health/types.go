// Package health holds the wire-format types shared between the CLI, the
// relay, and the iOS app. They mirror proto/schema.json — when the schema
// changes, this file changes with it.
package health

import "time"

// SampleType is a stable enum naming a HealthKit sample type. Only the iOS
// app knows how to translate it to an HKQuantityTypeIdentifier; on the CLI
// side it is opaque.
type SampleType string

// SampleType constants. The full set of HKQuantityTypeIdentifier-backed
// types is enumerated in catalog.go (one Definition per identifier);
// the constants below are the human-readable Go identifiers for those
// wire names. Editors adding a new HKQuantityTypeIdentifier should add
// the constant here AND a Catalog entry in catalog.go.
//
// SleepAnalysis and Workout are NOT quantity types — they are
// HKCategoryType / HKWorkoutType respectively — so they live outside
// the catalog.
const (
	// Activity
	StepCount                       SampleType = "step_count"
	ActiveEnergyBurned              SampleType = "active_energy_burned"
	BasalEnergyBurned               SampleType = "basal_energy_burned"
	DistanceWalkingRunning          SampleType = "distance_walking_running"
	DistanceCycling                 SampleType = "distance_cycling"
	DistanceSwimming                SampleType = "distance_swimming"
	DistanceWheelchair              SampleType = "distance_wheelchair"
	DistanceDownhillSnowSports      SampleType = "distance_downhill_snow_sports"
	DistanceCrossCountrySkiing      SampleType = "distance_cross_country_skiing"
	DistancePaddleSports            SampleType = "distance_paddle_sports"
	DistanceRowing                  SampleType = "distance_rowing"
	DistanceSkatingSports           SampleType = "distance_skating_sports"
	PushCount                       SampleType = "push_count"
	SwimmingStrokeCount             SampleType = "swimming_stroke_count"
	FlightsClimbed                  SampleType = "flights_climbed"
	AppleExerciseTime               SampleType = "apple_exercise_time"
	AppleMoveTime                   SampleType = "apple_move_time"
	AppleStandTime                  SampleType = "apple_stand_time"
	VO2Max                          SampleType = "vo2_max"
	RunningPower                    SampleType = "running_power"
	RunningSpeed                    SampleType = "running_speed"
	RunningStrideLength             SampleType = "running_stride_length"
	RunningVerticalOscillation      SampleType = "running_vertical_oscillation"
	RunningGroundContactTime        SampleType = "running_ground_contact_time"
	CyclingPower                    SampleType = "cycling_power"
	CyclingSpeed                    SampleType = "cycling_speed"
	CyclingCadence                  SampleType = "cycling_cadence"
	CyclingFunctionalThresholdPower SampleType = "cycling_functional_threshold_power"
	CrossCountrySkiingSpeed         SampleType = "cross_country_skiing_speed"
	PaddleSportsSpeed               SampleType = "paddle_sports_speed"
	RowingSpeed                     SampleType = "rowing_speed"
	PhysicalEffort                  SampleType = "physical_effort"
	WorkoutEffortScore              SampleType = "workout_effort_score"
	EstimatedWorkoutEffortScore     SampleType = "estimated_workout_effort_score"

	// Body measurements
	Height                        SampleType = "height"
	BodyMass                      SampleType = "body_mass"
	BodyMassIndex                 SampleType = "body_mass_index"
	LeanBodyMass                  SampleType = "lean_body_mass"
	BodyFatPercentage             SampleType = "body_fat_percentage"
	WaistCircumference            SampleType = "waist_circumference"
	AppleSleepingWristTemperature SampleType = "apple_sleeping_wrist_temperature"

	// Vital signs
	HeartRate                  SampleType = "heart_rate"
	HeartRateResting           SampleType = "heart_rate_resting"
	WalkingHeartRateAverage    SampleType = "walking_heart_rate_average"
	HeartRateVariabilitySDNN   SampleType = "heart_rate_variability_sdnn"
	HeartRateRecoveryOneMinute SampleType = "heart_rate_recovery_one_minute"
	AtrialFibrillationBurden   SampleType = "atrial_fibrillation_burden"
	OxygenSaturation           SampleType = "oxygen_saturation"
	BodyTemperature            SampleType = "body_temperature"
	BloodPressureSystolic      SampleType = "blood_pressure_systolic"
	BloodPressureDiastolic     SampleType = "blood_pressure_diastolic"
	RespiratoryRate            SampleType = "respiratory_rate"

	// Lab and test results
	BloodGlucose             SampleType = "blood_glucose"
	ElectrodermalActivity    SampleType = "electrodermal_activity"
	ForcedExpiratoryVolume1  SampleType = "forced_expiratory_volume_1"
	ForcedVitalCapacity      SampleType = "forced_vital_capacity"
	InhalerUsage             SampleType = "inhaler_usage"
	InsulinDelivery          SampleType = "insulin_delivery"
	NumberOfTimesFallen      SampleType = "number_of_times_fallen"
	PeakExpiratoryFlowRate   SampleType = "peak_expiratory_flow_rate"
	PeripheralPerfusionIndex SampleType = "peripheral_perfusion_index"

	// Nutrition
	DietaryEnergyConsumed     SampleType = "dietary_energy_consumed"
	DietaryWater              SampleType = "dietary_water"
	DietaryProtein            SampleType = "dietary_protein"
	DietaryCarbohydrates      SampleType = "dietary_carbohydrates"
	DietaryFiber              SampleType = "dietary_fiber"
	DietarySugar              SampleType = "dietary_sugar"
	DietaryFatTotal           SampleType = "dietary_fat_total"
	DietaryFatSaturated       SampleType = "dietary_fat_saturated"
	DietaryFatMonounsaturated SampleType = "dietary_fat_monounsaturated"
	DietaryFatPolyunsaturated SampleType = "dietary_fat_polyunsaturated"
	DietaryCholesterol        SampleType = "dietary_cholesterol"
	DietarySodium             SampleType = "dietary_sodium"
	DietaryPotassium          SampleType = "dietary_potassium"
	DietaryCalcium            SampleType = "dietary_calcium"
	DietaryIron               SampleType = "dietary_iron"
	DietaryMagnesium          SampleType = "dietary_magnesium"
	DietaryPhosphorus         SampleType = "dietary_phosphorus"
	DietaryZinc               SampleType = "dietary_zinc"
	DietaryCopper             SampleType = "dietary_copper"
	DietaryManganese          SampleType = "dietary_manganese"
	DietaryChromium           SampleType = "dietary_chromium"
	DietaryIodine             SampleType = "dietary_iodine"
	DietaryMolybdenum         SampleType = "dietary_molybdenum"
	DietarySelenium           SampleType = "dietary_selenium"
	DietaryChloride           SampleType = "dietary_chloride"
	DietaryCaffeine           SampleType = "dietary_caffeine"
	DietaryVitaminA           SampleType = "dietary_vitamin_a"
	DietaryVitaminB6          SampleType = "dietary_vitamin_b6"
	DietaryVitaminB12         SampleType = "dietary_vitamin_b12"
	DietaryVitaminC           SampleType = "dietary_vitamin_c"
	DietaryVitaminD           SampleType = "dietary_vitamin_d"
	DietaryVitaminE           SampleType = "dietary_vitamin_e"
	DietaryVitaminK           SampleType = "dietary_vitamin_k"
	DietaryThiamin            SampleType = "dietary_thiamin"
	DietaryRiboflavin         SampleType = "dietary_riboflavin"
	DietaryNiacin             SampleType = "dietary_niacin"
	DietaryFolate             SampleType = "dietary_folate"
	DietaryPantothenicAcid    SampleType = "dietary_pantothenic_acid"
	DietaryBiotin             SampleType = "dietary_biotin"

	// Hearing health
	EnvironmentalAudioExposure  SampleType = "environmental_audio_exposure"
	EnvironmentalSoundReduction SampleType = "environmental_sound_reduction"
	HeadphoneAudioExposure      SampleType = "headphone_audio_exposure"

	// Mobility
	AppleWalkingSteadiness         SampleType = "apple_walking_steadiness"
	WalkingSpeed                   SampleType = "walking_speed"
	WalkingStepLength              SampleType = "walking_step_length"
	WalkingAsymmetryPercentage     SampleType = "walking_asymmetry_percentage"
	WalkingDoubleSupportPercentage SampleType = "walking_double_support_percentage"
	StairAscentSpeed               SampleType = "stair_ascent_speed"
	StairDescentSpeed              SampleType = "stair_descent_speed"
	SixMinuteWalkTestDistance      SampleType = "six_minute_walk_test_distance"

	// Reproductive health (quantity)
	BasalBodyTemperature SampleType = "basal_body_temperature"

	// UV / daylight
	UVExposure     SampleType = "uv_exposure"
	TimeInDaylight SampleType = "time_in_daylight"

	// Diving
	UnderwaterDepth  SampleType = "underwater_depth"
	WaterTemperature SampleType = "water_temperature"

	// Alcohol
	BloodAlcoholContent        SampleType = "blood_alcohol_content"
	NumberOfAlcoholicBeverages SampleType = "number_of_alcoholic_beverages"

	// Sleep (extra quantity, iOS 18+)
	AppleSleepingBreathingDisturbances SampleType = "apple_sleeping_breathing_disturbances"

	// Non-quantity carryover (HKCategoryType + HKWorkoutType).
	SleepAnalysis SampleType = "sleep_analysis"
	Workout       SampleType = "workout"
)

// nonQuantitySampleTypes are the supported sample types that do NOT
// have a corresponding HKQuantityTypeIdentifier (and therefore are
// absent from Catalog). They are appended to AllSampleTypes by hand.
var nonQuantitySampleTypes = []SampleType{SleepAnalysis, Workout}

// allSampleTypes is built once at package init from Catalog +
// nonQuantitySampleTypes. validSampleTypes is the same data shaped as
// a set for O(1) IsValid() lookups.
var (
	allSampleTypes   = buildAllSampleTypes()
	validSampleTypes = buildValidSampleTypes()
)

func buildAllSampleTypes() []SampleType {
	out := make([]SampleType, 0, len(Catalog)+len(nonQuantitySampleTypes))
	for i := range Catalog {
		out = append(out, Catalog[i].Wire)
	}
	out = append(out, nonQuantitySampleTypes...)
	return out
}

func buildValidSampleTypes() map[SampleType]struct{} {
	m := make(map[SampleType]struct{}, len(allSampleTypes))
	for _, t := range allSampleTypes {
		m[t] = struct{}{}
	}
	return m
}

// AllSampleTypes lists every supported sample type — every
// HKQuantityTypeIdentifier in Catalog plus the non-quantity carryover
// (sleep_analysis, workout). The returned slice is a defensive copy
// so callers can mutate it without corrupting the package-level
// cache.
func AllSampleTypes() []SampleType {
	out := make([]SampleType, len(allSampleTypes))
	copy(out, allSampleTypes)
	return out
}

// IsValid returns true if t is one of the supported sample types.
func (t SampleType) IsValid() bool {
	_, ok := validSampleTypes[t]
	return ok
}

// Source describes which app produced a sample. Filled in by the iOS app on
// reads; ignored on writes.
type Source struct {
	Name     string `json:"name,omitempty"`
	BundleID string `json:"bundle_id,omitempty"`
}

// Sample is one HealthKit sample.
type Sample struct {
	UUID     string         `json:"uuid,omitempty"`
	Type     SampleType     `json:"type"`
	Value    float64        `json:"value"`
	Unit     string         `json:"unit"`
	Start    time.Time      `json:"start"`
	End      time.Time      `json:"end"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Source   *Source        `json:"source,omitempty"`
}

// JobKind enumerates the supported jobs the iOS app can execute.
type JobKind string

const (
	KindRead  JobKind = "read"
	KindWrite JobKind = "write"
	KindSync  JobKind = "sync"
)

// ReadPayload is the plaintext payload for a `read` job.
type ReadPayload struct {
	Type  SampleType `json:"type"`
	From  time.Time  `json:"from"`
	To    time.Time  `json:"to"`
	Limit int        `json:"limit,omitempty"`
}

// ReadResult is what comes back from a successful `read`.
type ReadResult struct {
	Type    SampleType `json:"type"`
	Samples []Sample   `json:"samples"`
}

// WritePayload is the plaintext payload for a `write` job.
type WritePayload struct {
	Sample Sample `json:"sample"`
}

// WriteResult is what comes back from a successful `write`.
type WriteResult struct {
	UUID string `json:"uuid"`
}

// SyncPayload is the plaintext payload for a `sync` job. Anchors are opaque
// base64 blobs whose meaning only the iOS app understands; an empty/missing
// entry means "full sync that type".
type SyncPayload struct {
	Types   []SampleType      `json:"types"`
	Anchors map[string]string `json:"anchors,omitempty"`
}

// SyncResultPage is one page of an anchored sync. Multiple pages may share
// a single job_id; the CLI reassembles them.
type SyncResultPage struct {
	Type       SampleType `json:"type"`
	PageIndex  int        `json:"page_index"`
	Added      []Sample   `json:"added,omitempty"`
	Deleted    []string   `json:"deleted,omitempty"`
	NextAnchor string     `json:"next_anchor,omitempty"`
	More       bool       `json:"more"`
}

// Job is the plaintext envelope an agent / CLI submits.
type Job struct {
	ID        string    `json:"id"`
	Kind      JobKind   `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	Deadline  time.Time `json:"deadline,omitempty"`
	// Payload is one of ReadPayload / WritePayload / SyncPayload, json-encoded
	// as an object. We keep it as RawMessage at the relay layer; the CLI and
	// iOS app type-assert based on Kind.
	Payload any `json:"payload"`
}

// ResultStatus describes whether a result page reports success or failure.
type ResultStatus string

const (
	StatusDone   ResultStatus = "done"
	StatusFailed ResultStatus = "failed"
)

// JobError is the structured error a result can carry.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result is one page of a job's response. Read and write jobs only ever
// produce one page; sync jobs may produce many.
type Result struct {
	JobID     string       `json:"job_id"`
	PageIndex int          `json:"page_index"`
	Status    ResultStatus `json:"status"`
	// Result is one of ReadResult / WriteResult / SyncResultPage, depending
	// on the originating Job.Kind. Carried as `any` because the CLI and iOS
	// app know the kind out-of-band.
	Result any       `json:"result,omitempty"`
	Error  *JobError `json:"error,omitempty"`
}
