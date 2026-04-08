package fakerelay

import (
	"context"
	"fmt"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// JobHandler is what the fake iOS app side runs against each decoded job.
// Returns a *health.Result for single-page jobs (read, write); use
// MultiPageJobHandler for sync jobs that may produce many pages keyed to
// the same job_id.
type JobHandler func(ctx context.Context, job *health.Job) (*health.Result, error)

// MultiPageJobHandler is the variant for sync-style jobs that may emit
// multiple result pages. Each entry will be posted as its own /v1/results
// blob with its own page_index.
type MultiPageJobHandler func(ctx context.Context, job *health.Job) ([]*health.Result, error)

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
	// MultiPage is consulted first; falls back to Handler when nil.
	// If MultiPage returns an error, the drainer wraps it as a failed
	// page just like Handler errors.
	MultiPage MultiPageJobHandler
	cursor    int64
}

// NewDrainer wires a drainer to an existing relay client and a session
// holding the shared key. The handler will be called once per drained job.
func NewDrainer(c *relay.Client, s *jobs.Session, h JobHandler) *FakeIOSDrainer {
	return &FakeIOSDrainer{Client: c, Session: s, Handler: h}
}

// NewMultiPageDrainer is the multi-page variant used by sync scenario tests.
func NewMultiPageDrainer(c *relay.Client, s *jobs.Session, h MultiPageJobHandler) *FakeIOSDrainer {
	return &FakeIOSDrainer{Client: c, Session: s, MultiPage: h}
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
		if page.NextCursor > d.cursor {
			d.cursor = page.NextCursor
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
		// If the handler returned a result without explicitly setting
		// PageIndex, fall back to the order in the slice.
		pageIndex := res.PageIndex
		if pageIndex == 0 && i > 0 {
			pageIndex = i
		}
		blob, err := d.Session.SealResult(job.ID, pageIndex, res)
		if err != nil {
			return fmt.Errorf("drainer: seal result: %w", err)
		}
		// Mirror the iOS drain loop's persistence policy: read/sync
		// → ephemeral, write/profile/failed → persistent. Failures are
		// always persisted so the agent never silently loses an error
		// report.
		persistent := persistentForJobAndResult(job.Kind, res.Status)
		if _, err := d.Client.PostResult(ctx, job.ID, pageIndex, blob, persistent); err != nil {
			return fmt.Errorf("drainer: post result: %w", err)
		}
	}
	return nil
}

// persistentForJobAndResult mirrors the iOS-side
// HealthBridgeApp.shouldPersistResult policy: read/sync results are
// ephemeral (large blobs, synchronous CLI on the other end);
// write/profile results are persistent (tiny blobs the agent may
// come back to retrieve); a failed status overrides kind so error
// reports survive a Durable Object eviction.
func persistentForJobAndResult(kind health.JobKind, status health.ResultStatus) bool {
	if status == health.StatusFailed {
		return true
	}
	switch kind {
	case health.KindRead, health.KindSync:
		return false
	case health.KindWrite, health.KindProfile:
		return true
	}
	return true
}

func (d *FakeIOSDrainer) runHandler(ctx context.Context, job *health.Job) ([]*health.Result, error) {
	if d.MultiPage != nil {
		return d.MultiPage(ctx, job)
	}
	if d.Handler != nil {
		res, err := d.Handler(ctx, job)
		if err != nil {
			return nil, err
		}
		return []*health.Result{res}, nil
	}
	return nil, fmt.Errorf("drainer: no handler configured")
}
