package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestJobsMirrorRoundTrip exercises the local jobs SQLite mirror through
// the read subcommand body. After a successful round trip the mirror
// should record one entry with status=done.
func TestJobsMirrorRoundTrip(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000050"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	store, err := jobs.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	handler := func(_ context.Context, job *health.Job) (*health.Result, error) {
		if job.Kind != health.KindRead {
			return nil, errors.New("expected read job")
		}
		return &health.Result{
			Status: health.StatusDone,
			Result: health.ReadResult{Type: health.StepCount, Samples: []health.Sample{}},
		}, nil
	}
	drainer := fakerelay.NewDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()

	job := jobs.NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	var out bytes.Buffer
	if err := executeReadJob(ctx, &out, cliClient, session, store, job, 2*time.Second, true); err != nil {
		t.Fatalf("executeReadJob: %v", err)
	}

	rec, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("mirror missing job %s: %v", job.ID, err)
	}
	if rec.Status != jobs.StatusDone {
		t.Errorf("status = %q, want done", rec.Status)
	}
	if rec.PairID != pairID {
		t.Errorf("pair_id = %q, want %q", rec.PairID, pairID)
	}
	if !strings.Contains(string(rec.PayloadJSON), "step_count") {
		t.Errorf("payload should contain sample type, got: %s", rec.PayloadJSON)
	}
}

// TestJobsMirrorRecordsPendingWhenOffline ensures the mirror gets a
// pending row even when no drainer is around.
func TestJobsMirrorRecordsPendingWhenOffline(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()
	pairID := "01J9ZX0PAIR000000000000051"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	store, err := jobs.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	job := jobs.NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	var out bytes.Buffer
	if err := executeReadJob(context.Background(), &out, cliClient, session, store, job, 0, true); err != nil {
		t.Fatalf("executeReadJob: %v", err)
	}
	rec, err := store.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != jobs.StatusPending {
		t.Errorf("status = %q, want pending", rec.Status)
	}
}

// TestJobsMirrorRecordsFailureFromDrainer verifies that a structured
// failed result is captured in the mirror's error_code/error_message.
func TestJobsMirrorRecordsFailureFromDrainer(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()
	pairID := "01J9ZX0PAIR000000000000052"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	store, err := jobs.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	drainer := fakerelay.NewDrainer(drainerClient, session, func(_ context.Context, _ *health.Job) (*health.Result, error) {
		return &health.Result{
			Status: health.StatusFailed,
			Error:  &health.JobError{Code: "scope_denied", Message: "no auth"},
		}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()

	job := jobs.NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	var out bytes.Buffer
	_ = executeReadJob(ctx, &out, cliClient, session, store, job, 2*time.Second, true)
	rec, err := store.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != jobs.StatusFailed {
		t.Errorf("status = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "scope_denied" {
		t.Errorf("error_code = %q, want scope_denied", rec.ErrorCode)
	}
}
