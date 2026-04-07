package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestWriteScenarioRoundTrip drives a write end-to-end through the
// fakerelay: the CLI executes the write subcommand body, the FakeIOSDrainer
// receives the WritePayload, validates it, returns a synthetic UUID, and
// the CLI prints the UUID back to the user.
func TestWriteScenarioRoundTrip(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000020"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	var seenSample atomic.Pointer[health.Sample]

	handler := func(_ context.Context, job *health.Job) (*health.Result, error) {
		if job.Kind != health.KindWrite {
			return nil, errors.New("expected write job")
		}
		// The drainer hands us a typed Job whose Payload is map[string]any
		// after JSON round-trip; re-decode into the typed struct.
		pb, err := json.Marshal(job.Payload)
		if err != nil {
			return nil, err
		}
		var wp health.WritePayload
		if err := json.Unmarshal(pb, &wp); err != nil {
			return nil, err
		}
		seenSample.Store(&wp.Sample)
		return &health.Result{
			Status: health.StatusDone,
			Result: health.WriteResult{UUID: "fake-healthkit-uuid-123"},
		}, nil
	}

	drainer := fakerelay.NewDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()
	defer cancel()

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindWrite,
		CreatedAt: time.Now().UTC(),
		Payload: health.WritePayload{
			Sample: health.Sample{
				Type:  health.DietaryEnergyConsumed,
				Value: 500,
				Unit:  "kcal",
				Start: time.Date(2026, 4, 7, 12, 30, 0, 0, time.UTC),
				End:   time.Date(2026, 4, 7, 12, 30, 0, 0, time.UTC),
			},
		},
	}

	var out bytes.Buffer
	if err := executeWriteJob(ctx, &out, cliClient, session, nil, job, 2*time.Second, true); err != nil {
		t.Fatalf("executeWriteJob: %v", err)
	}

	got := seenSample.Load()
	if got == nil {
		t.Fatal("drainer never saw a sample")
	}
	if got.Type != health.DietaryEnergyConsumed {
		t.Errorf("sample type = %q, want dietary_energy_consumed", got.Type)
	}
	if got.Value != 500 {
		t.Errorf("sample value = %v, want 500", got.Value)
	}
	if got.Unit != "kcal" {
		t.Errorf("sample unit = %q, want kcal", got.Unit)
	}

	var resp struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
		UUID   string `json:"uuid"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if resp.Status != "done" || resp.UUID != "fake-healthkit-uuid-123" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestParseMetaFlags(t *testing.T) {
	cases := []struct {
		in      []string
		want    map[string]any
		wantErr bool
	}{
		{nil, nil, false},
		{[]string{"a=1"}, map[string]any{"a": "1"}, false},
		{[]string{"source=manual", "comment=lunch"}, map[string]any{"source": "manual", "comment": "lunch"}, false},
		{[]string{"bad"}, nil, true},
		{[]string{"=value"}, nil, true},
	}
	for _, tc := range cases {
		got, err := parseMetaFlags(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMetaFlags(%v): expected error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMetaFlags(%v): unexpected error %v", tc.in, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseMetaFlags(%v): len = %d, want %d", tc.in, len(got), len(tc.want))
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("parseMetaFlags(%v)[%q] = %v, want %v", tc.in, k, got[k], v)
			}
		}
	}
}

// TestWriteScenarioFailedResult: a structured failure from the iOS handler
// should propagate as an exit error containing the error code.
func TestWriteScenarioFailedResult(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()
	pairID := "01J9ZX0PAIR000000000000021"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	handler := func(_ context.Context, _ *health.Job) (*health.Result, error) {
		return &health.Result{
			Status: health.StatusFailed,
			Error: &health.JobError{
				Code:    "scope_denied",
				Message: "dietary_energy_consumed is not authorised",
			},
		}, nil
	}
	drainer := fakerelay.NewDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()

	job := &health.Job{
		ID:   jobs.NewID(),
		Kind: health.KindWrite,
		Payload: health.WritePayload{
			Sample: health.Sample{
				Type: health.DietaryEnergyConsumed, Value: 500, Unit: "kcal",
				Start: time.Now().UTC(), End: time.Now().UTC(),
			},
		},
	}
	var out bytes.Buffer
	err := executeWriteJob(ctx, &out, cliClient, session, nil, job, 2*time.Second, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "scope_denied") {
		t.Errorf("error = %v, want it to contain scope_denied", err)
	}
}
