package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestWriteScenarioRoundTrip drives a write end-to-end through the
// fakerelay. The new contract for `healthbridge write` is fire-and-
// forget: executeWriteJob returns immediately with `pending`, never
// long-polls for the result. We assert:
//
//   1. The CLI emitted a `pending` JSON response with the right job_id.
//   2. The fakerelay drainer eventually picks up the job and sees the
//      typed WritePayload with the original sample.
//   3. The drainer's posted result is retrievable from the relay by
//      anyone who later does the `jobs wait`-equivalent (PollResults
//      + session.OpenResult), proving the persistent path works.
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

	// 1. Write should return immediately with pending — no long-poll.
	var out bytes.Buffer
	if err := executeWriteJob(ctx, &out, cliClient, session, nil, job, 2*time.Second, true); err != nil {
		t.Fatalf("executeWriteJob: %v", err)
	}
	var resp struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if resp.Status != "pending" {
		t.Errorf("expected pending, got %q in %s", resp.Status, out.String())
	}
	if resp.JobID != job.ID {
		t.Errorf("job_id = %q, want %q", resp.JobID, job.ID)
	}

	// 2. The drainer goroutine should eventually pick up the job and
	//    record the typed sample. Poll the atomic for up to ~3s.
	var got *health.Sample
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if g := seenSample.Load(); g != nil {
			got = g
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil {
		t.Fatal("drainer never saw a sample after the fire-and-forget enqueue")
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

	// 3. The drainer's posted result should be retrievable via the
	//    relay's poll endpoint, proving the persistent path is
	//    intact and a later `jobs wait` would find it.
	pollCtx, pollCancel := context.WithTimeout(ctx, 2*time.Second)
	defer pollCancel()
	pollResp, err := cliClient.PollResults(pollCtx, job.ID, 2000)
	if err != nil {
		t.Fatalf("poll results: %v", err)
	}
	if len(pollResp.Results) == 0 {
		t.Fatal("expected the persistent write result to still be on the relay")
	}
	first := pollResp.Results[0]
	result, err := session.OpenResult(first.JobID, first.PageIndex, first.Blob)
	if err != nil {
		t.Fatalf("open result: %v", err)
	}
	pb, _ := json.Marshal(result.Result)
	var wr health.WriteResult
	if err := json.Unmarshal(pb, &wr); err != nil {
		t.Fatalf("decode write result: %v", err)
	}
	if wr.UUID != "fake-healthkit-uuid-123" {
		t.Errorf("uuid = %q, want fake-healthkit-uuid-123", wr.UUID)
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

// TestWriteScenarioFailedResult: a write whose iOS handler returns a
// structured failure should NOT surface synchronously from the
// `write` subcommand (writes are fire-and-forget). The failure must
// still be retrievable from the relay so a follow-up `jobs wait`
// surfaces it to the user. We assert both halves: the write call
// succeeds and prints `pending`, and the failed result is then
// readable through the relay's poll path.
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
	// The write call itself succeeds — fire-and-forget, no failure
	// propagation here.
	if err := executeWriteJob(ctx, &out, cliClient, session, nil, job, 2*time.Second, true); err != nil {
		t.Fatalf("executeWriteJob unexpectedly errored: %v", err)
	}
	var pendingResp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &pendingResp); err != nil {
		t.Fatalf("decode CLI output: %v\n%s", err, out.String())
	}
	if pendingResp.Status != "pending" {
		t.Errorf("expected pending status in CLI output, got %q in %s", pendingResp.Status, out.String())
	}

	// The failure is still observable via the relay poll path that
	// `jobs wait` would use.
	pollCtx, pollCancel := context.WithTimeout(ctx, 2*time.Second)
	defer pollCancel()
	pollResp, err := cliClient.PollResults(pollCtx, job.ID, 2000)
	if err != nil {
		t.Fatalf("poll results: %v", err)
	}
	if len(pollResp.Results) == 0 {
		t.Fatal("expected the persistent failure result to be on the relay")
	}
	first := pollResp.Results[0]
	result, err := session.OpenResult(first.JobID, first.PageIndex, first.Blob)
	if err != nil {
		t.Fatalf("open result: %v", err)
	}
	if result.Status != health.StatusFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "scope_denied" {
		t.Errorf("error = %+v, want scope_denied", result.Error)
	}
}
