package health

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSampleTypeIsValid(t *testing.T) {
	cases := []struct {
		in   SampleType
		want bool
	}{
		{StepCount, true},
		{DietaryEnergyConsumed, true},
		{Workout, true},
		{SleepAnalysis, true},
		{"", false},
		{"not_a_real_type", false},
		{"STEP_COUNT", false},
	}
	for _, tc := range cases {
		if got := tc.in.IsValid(); got != tc.want {
			t.Errorf("SampleType(%q).IsValid() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAllSampleTypesAreValid(t *testing.T) {
	for _, st := range AllSampleTypes() {
		if !st.IsValid() {
			t.Errorf("AllSampleTypes() contained %q which IsValid() rejects", st)
		}
	}
}

func TestJobJSONRoundTrip(t *testing.T) {
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	original := Job{
		ID:        "01J9ZX0EXAMPLE0000000000001",
		Kind:      KindRead,
		CreatedAt: time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
		Payload: ReadPayload{
			Type:  StepCount,
			From:  from,
			To:    to,
			Limit: 1000,
		},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Decode back as a generic Job; payload is `any`, so it'll come back as
	// map[string]any. We re-marshal/unmarshal the payload to a typed value to
	// confirm field names match.
	var decoded Job
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ID != original.ID || decoded.Kind != original.Kind {
		t.Errorf("decoded envelope mismatch: %+v vs %+v", decoded, original)
	}
	pb, _ := json.Marshal(decoded.Payload)
	var rp ReadPayload
	if err := json.Unmarshal(pb, &rp); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if rp.Type != StepCount || !rp.From.Equal(from) || !rp.To.Equal(to) || rp.Limit != 1000 {
		t.Errorf("payload round-trip mismatch: %+v", rp)
	}
}

func TestResultJSONOmitsEmpty(t *testing.T) {
	r := Result{
		JobID:     "01J9ZX0EXAMPLE0000000000001",
		PageIndex: 0,
		Status:    StatusDone,
		Result:    ReadResult{Type: StepCount, Samples: nil},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if want := `"status":"done"`; !contains(s, want) {
		t.Errorf("missing %q in %s", want, s)
	}
	if contains(s, `"error"`) {
		t.Errorf("expected error to be omitted in success: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
