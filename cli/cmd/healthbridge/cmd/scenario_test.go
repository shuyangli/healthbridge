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

// TestReadScenarioRoundTrip drives the full M1 path:
//
//   1. A fake relay (httptest) stands in for the Cloudflare Worker.
//   2. A FakeIOSDrainer goroutine plays the iOS app: it polls for jobs,
//      decodes them, runs a stub HealthKit handler, and posts a result.
//   3. The CLI's executeReadJob (the body of the `read` subcommand) is
//      called directly with a buffer for stdout, the same way `cobra` would
//      invoke it from the command line.
//
// We assert that the read round-trips: the CLI emits a sample matching what
// the stub HealthKit handler returned.
func TestReadScenarioRoundTrip(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000001"
	cliClient := relay.New(server.URL(), pairID)
	drainerClient := relay.New(server.URL(), pairID)

	var seenJob atomic.Pointer[health.Job]

	// The stub iOS-side handler. It captures the job, then synthesises a
	// canned step_count sample so we can verify the CLI prints it back.
	handler := func(ctx context.Context, job *health.Job) (*health.Result, error) {
		seenJob.Store(job)
		if job.Kind != health.KindRead {
			return nil, errors.New("expected read job")
		}
		return &health.Result{
			JobID:     job.ID,
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
		}, nil
	}

	drainer := fakerelay.NewDrainer(drainerClient, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	drainErr := make(chan error, 1)
	go func() {
		drainErr <- drainer.Run(ctx)
	}()

	job := jobs.NewReadJob(
		health.StepCount,
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
	)

	var out bytes.Buffer
	if err := executeReadJob(ctx, &out, cliClient, job, 2*time.Second, true /* json */); err != nil {
		t.Fatalf("executeReadJob: %v", err)
	}

	// Stop the drainer goroutine cleanly.
	cancel()
	if err := <-drainErr; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		// Drainer poll may also return a cancelled-context error wrapped by
		// the relay client; tolerate any "context" wording.
		if !strings.Contains(err.Error(), "context") {
			t.Fatalf("drainer: %v", err)
		}
	}

	// Sanity-check the captured job.
	captured := seenJob.Load()
	if captured == nil {
		t.Fatal("drainer never saw a job")
	}
	if captured.ID != job.ID {
		t.Errorf("captured job ID = %q, want %q", captured.ID, job.ID)
	}

	// Parse the JSON output and assert on shape.
	var got struct {
		JobID   string          `json:"job_id"`
		Status  string          `json:"status"`
		Type    string          `json:"type"`
		Samples []health.Sample `json:"samples"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode CLI JSON output: %v\n--- raw ---\n%s", err, out.String())
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Type != string(health.StepCount) {
		t.Errorf("type = %q, want step_count", got.Type)
	}
	if len(got.Samples) != 1 {
		t.Fatalf("samples len = %d, want 1", len(got.Samples))
	}
	if got.Samples[0].Value != 8421 {
		t.Errorf("sample value = %v, want 8421", got.Samples[0].Value)
	}
	if got.JobID != job.ID {
		t.Errorf("job_id = %q, want %q", got.JobID, job.ID)
	}
}

// TestReadScenarioPendingWhenDrainerOffline verifies the offline-tolerant
// path: the CLI returns `pending` if no drainer is running.
func TestReadScenarioPendingWhenDrainerOffline(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000002"
	cliClient := relay.New(server.URL(), pairID)

	job := jobs.NewReadJob(
		health.StepCount,
		time.Now().Add(-24*time.Hour),
		time.Now(),
	)

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// --wait 0 makes this a fire-and-forget call. With no drainer running,
	// the CLI should print a pending status and exit cleanly.
	if err := executeReadJob(ctx, &out, cliClient, job, 0, true); err != nil {
		t.Fatalf("executeReadJob: %v", err)
	}

	var got struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode pending JSON: %v\n%s", err, out.String())
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.JobID != job.ID {
		t.Errorf("job_id = %q, want %q", got.JobID, job.ID)
	}

	// The job is still in the relay's mailbox waiting to be drained.
	if n := server.PendingJobCount(); n != 1 {
		t.Errorf("server should have 1 pending job, has %d", n)
	}
}

// TestReadScenarioFailedResult verifies that a structured error from the
// iOS handler propagates back as an exit error from the CLI.
func TestReadScenarioFailedResult(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000003"
	cliClient := relay.New(server.URL(), pairID)
	drainerClient := relay.New(server.URL(), pairID)

	handler := func(_ context.Context, job *health.Job) (*health.Result, error) {
		return &health.Result{
			JobID:  job.ID,
			Status: health.StatusFailed,
			Error: &health.JobError{
				Code:    "scope_denied",
				Message: "step_count not authorised",
			},
		}, nil
	}
	drainer := fakerelay.NewDrainer(drainerClient, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()
	defer cancel()

	job := jobs.NewReadJob(health.StepCount, time.Now().Add(-1*time.Hour), time.Now())
	var out bytes.Buffer
	err := executeReadJob(ctx, &out, cliClient, job, 2*time.Second, false)
	if err == nil {
		t.Fatal("expected an error from a failed result")
	}
	if !strings.Contains(err.Error(), "scope_denied") {
		t.Errorf("error = %v, want it to contain scope_denied", err)
	}
}
