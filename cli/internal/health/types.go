// Package health holds the wire-format types shared between the CLI, the
// relay, and the iOS app. They mirror proto/schema.json — when the schema
// changes, this file changes with it.
package health

import "time"

// SampleType is a stable enum naming a HealthKit sample type. Only the iOS
// app knows how to translate it to an HKQuantityTypeIdentifier; on the CLI
// side it is opaque.
type SampleType string

const (
	StepCount             SampleType = "step_count"
	ActiveEnergyBurned    SampleType = "active_energy_burned"
	BasalEnergyBurned     SampleType = "basal_energy_burned"
	HeartRate             SampleType = "heart_rate"
	HeartRateResting      SampleType = "heart_rate_resting"
	BodyMass              SampleType = "body_mass"
	BodyMassIndex         SampleType = "body_mass_index"
	BloodGlucose          SampleType = "blood_glucose"
	DietaryEnergyConsumed SampleType = "dietary_energy_consumed"
	DietaryWater          SampleType = "dietary_water"
	SleepAnalysis         SampleType = "sleep_analysis"
	Workout               SampleType = "workout"
)

// AllSampleTypes lists every supported sample type. Used by `healthbridge
// types` and by validation.
func AllSampleTypes() []SampleType {
	return []SampleType{
		StepCount, ActiveEnergyBurned, BasalEnergyBurned,
		HeartRate, HeartRateResting,
		BodyMass, BodyMassIndex, BloodGlucose,
		DietaryEnergyConsumed, DietaryWater,
		SleepAnalysis, Workout,
	}
}

// IsValid returns true if t is one of the supported sample types.
func (t SampleType) IsValid() bool {
	for _, known := range AllSampleTypes() {
		if t == known {
			return true
		}
	}
	return false
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
