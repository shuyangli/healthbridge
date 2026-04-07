package jobs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func TestNewIDIsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewID()
		if id == "" {
			t.Fatal("empty id")
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d iterations", id, i)
		}
		seen[id] = true
	}
}

func TestEncodeDecodeJobRoundTrip(t *testing.T) {
	original := NewReadJob(health.StepCount,
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC))

	blob, err := EncodeJob(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if blob == "" {
		t.Fatal("empty blob")
	}

	decoded, err := DecodeJob(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ID != original.ID || decoded.Kind != original.Kind {
		t.Errorf("envelope mismatch: %+v vs %+v", decoded, original)
	}

	// Round-trip the payload through json so we can compare typed values.
	pb, _ := json.Marshal(decoded.Payload)
	var rp health.ReadPayload
	if err := json.Unmarshal(pb, &rp); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if rp.Type != health.StepCount {
		t.Errorf("payload type = %q, want step_count", rp.Type)
	}
}

func TestEncodeDecodeResultRoundTrip(t *testing.T) {
	original := &health.Result{
		JobID:     "abc123",
		PageIndex: 0,
		Status:    health.StatusDone,
		Result: health.ReadResult{
			Type: health.StepCount,
			Samples: []health.Sample{
				{
					Type:  health.StepCount,
					Value: 8421,
					Unit:  "count",
					Start: time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	}

	blob, err := EncodeResult(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeResult(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.JobID != original.JobID || decoded.Status != health.StatusDone {
		t.Errorf("mismatch: %+v", decoded)
	}
}

func TestDecodeJobRejectsGarbage(t *testing.T) {
	if _, err := DecodeJob("not_base64!!!"); err == nil {
		t.Error("expected error on garbage input")
	}
	if _, err := DecodeJob("aGVsbG8="); err == nil {
		// "hello" is valid base64 but not valid JSON
		t.Error("expected error on non-JSON content")
	}
}
