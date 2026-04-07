package jobs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/health"
)

// newTestSession returns a Session with a deterministic 32-byte key so the
// tests don't depend on a real X25519 exchange. Other tests in the
// internal/crypto package cover the key-agreement path; here we only care
// about the codec.
func newTestSession(t *testing.T) *Session {
	t.Helper()
	key := bytes.Repeat([]byte{0xab}, crypto.SessionKeySize)
	return &Session{Key: key, PairID: "01J9ZX0PAIR000000000000001"}
}

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

func TestSealAndOpenJobRoundTrip(t *testing.T) {
	s := newTestSession(t)
	original := NewReadJob(
		health.StepCount,
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
	)

	blob, err := s.SealJob(original)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if blob == "" {
		t.Fatal("empty blob")
	}

	decoded, err := s.OpenJob(original.ID, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if decoded.ID != original.ID || decoded.Kind != original.Kind {
		t.Errorf("envelope mismatch: %+v vs %+v", decoded, original)
	}

	pb, _ := json.Marshal(decoded.Payload)
	var rp health.ReadPayload
	if err := json.Unmarshal(pb, &rp); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if rp.Type != health.StepCount {
		t.Errorf("payload type = %q, want step_count", rp.Type)
	}
}

func TestOpenJobRejectsWrongJobID(t *testing.T) {
	s := newTestSession(t)
	job := NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	blob, err := s.SealJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenJob("a-different-id", blob); err == nil {
		t.Error("expected AAD mismatch error when job_id is wrong")
	}
}

func TestOpenJobRejectsWrongPair(t *testing.T) {
	a := newTestSession(t)
	b := &Session{Key: a.Key, PairID: "01J9ZX0PAIR000000000000999"}
	job := NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	blob, err := a.SealJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.OpenJob(job.ID, blob); err == nil {
		t.Error("expected AAD mismatch when pair_id differs")
	}
}

func TestOpenJobRejectsWrongKey(t *testing.T) {
	s := newTestSession(t)
	wrongKey := bytes.Repeat([]byte{0xcd}, crypto.SessionKeySize)
	wrong := &Session{Key: wrongKey, PairID: s.PairID}
	job := NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	blob, _ := s.SealJob(job)
	if _, err := wrong.OpenJob(job.ID, blob); err == nil {
		t.Error("expected error when key is wrong")
	}
}

func TestSealAndOpenResultRoundTrip(t *testing.T) {
	s := newTestSession(t)
	original := &health.Result{
		Status: health.StatusDone,
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

	blob, err := s.SealResult("job-1", 0, original)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	decoded, err := s.OpenResult("job-1", 0, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if decoded.JobID != "job-1" || decoded.PageIndex != 0 || decoded.Status != health.StatusDone {
		t.Errorf("result envelope mismatch: %+v", decoded)
	}
}

func TestOpenResultRejectsWrongPageIndex(t *testing.T) {
	s := newTestSession(t)
	r := &health.Result{Status: health.StatusDone}
	blob, _ := s.SealResult("job-1", 0, r)
	if _, err := s.OpenResult("job-1", 1, blob); err == nil {
		t.Error("expected AAD mismatch on page_index")
	}
}

func TestSealRejectsBadSession(t *testing.T) {
	bad := &Session{Key: []byte{0x01, 0x02}, PairID: "p"}
	_, err := bad.SealJob(NewReadJob(health.StepCount, time.Now(), time.Now().Add(time.Hour)))
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("expected key-size error, got %v", err)
	}
}

func TestSealedBlobLooksRandom(t *testing.T) {
	s := newTestSession(t)
	job := NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	blob, _ := s.SealJob(job)
	// The plaintext JSON contains the literal "step_count"; the sealed
	// blob should NOT, since it's encrypted.
	if strings.Contains(blob, "step_count") {
		t.Errorf("sealed blob leaks plaintext: %s", blob)
	}
}
