package fakerelay

import (
	"context"
	"fmt"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// JobHandler is what the fake iOS app side runs against each decoded job.
// Returns a *health.Result for single-page jobs (read, write, profile).
type JobHandler func(ctx context.Context, job *health.Job) (*health.Result, error)

// FakeIOSDrainer mimics the iOS HealthBridge app: it long-polls the relay
// for jobs, decodes each one, runs a handler, and posts the result back.
// As of M2 it uses an encrypted Session for both directions.
//
// The drainer is intentionally minimal — no HealthKit, no audit log. Tests
// use it as a programmable stand-in for the iPhone.
type FakeIOSDrainer struct {
	Client  *relay.Client
	Session *jobs.Session
	Handler JobHandler
	cursor  int64
}

// NewDrainer wires a drainer to an existing relay client and a session
// holding the shared key. The handler will be called once per drained job.
func NewDrainer(c *relay.Client, s *jobs.Session, h JobHandler) *FakeIOSDrainer {
	return &FakeIOSDrainer{Client: c, Session: s, Handler: h}
}

// Run loops until ctx is cancelled. Each iteration polls for one batch of
// jobs and processes them serially.
func (d *FakeIOSDrainer) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := d.Client.PollJobs(ctx, d.cursor, 5_000)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("drainer: poll jobs: %w", err)
		}
		for _, jb := range page.Jobs {
			if err := d.processOne(ctx, jb); err != nil {
				return err
			}
		}
		if page.NextCursorMs > d.cursor {
			d.cursor = page.NextCursorMs
		}
	}
}

func (d *FakeIOSDrainer) processOne(ctx context.Context, jb relay.JobBlob) error {
	job, err := d.Session.OpenJob(jb.JobID, jb.Blob)
	if err != nil {
		return fmt.Errorf("drainer: open job %s: %w", jb.JobID, err)
	}
	pages, err := d.runHandler(ctx, job)
	if err != nil {
		pages = []*health.Result{{
			Status: health.StatusFailed,
			Error: &health.JobError{
				Code:    "handler_error",
				Message: err.Error(),
			},
		}}
	}
	for i, res := range pages {
		blob, err := d.Session.SealResult(job.ID, i, res)
		if err != nil {
			return fmt.Errorf("drainer: seal result: %w", err)
		}
		// Mirror the iOS drain loop's persistence policy: read results
		// are ephemeral (large blobs, synchronous CLI); write/profile
		// results are persistent (tiny, agent may retrieve later).
		// Failed status is always persisted so the agent never silently
		// loses an error report.
		persistent := persistentForJobAndResult(job.Kind, res.Status)
		if _, err := d.Client.PostResult(ctx, job.ID, i, blob, persistent); err != nil {
			return fmt.Errorf("drainer: post result: %w", err)
		}
	}
	return nil
}

func persistentForJobAndResult(kind health.JobKind, status health.ResultStatus) bool {
	if status == health.StatusFailed {
		return true
	}
	switch kind {
	case health.KindRead:
		return false
	case health.KindWrite, health.KindProfile:
		return true
	}
	return true
}

func (d *FakeIOSDrainer) runHandler(ctx context.Context, job *health.Job) ([]*health.Result, error) {
	if d.Handler == nil {
		return nil, fmt.Errorf("drainer: no handler configured")
	}
	res, err := d.Handler(ctx, job)
	if err != nil {
		return nil, err
	}
	return []*health.Result{res}, nil
}
