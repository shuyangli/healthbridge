package fakerelay

import (
	"context"
	"fmt"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// JobHandler is what the fake iOS app side runs against each decoded job.
// Tests provide one to assert on the payload and produce a result.
type JobHandler func(ctx context.Context, job *health.Job) (*health.Result, error)

// FakeIOSDrainer mimics the iOS HealthBridge app: it long-polls the relay
// for jobs, decodes each one, runs a handler, and posts the result back.
//
// The drainer is intentionally minimal — no HealthKit, no encryption (yet),
// no audit log. Tests use it as a programmable stand-in for the iPhone.
type FakeIOSDrainer struct {
	Client  *relay.Client
	Handler JobHandler
	cursor  int64
}

// NewDrainer wires a drainer to an existing relay client (which already
// knows the pair_id). The handler will be called once per drained job.
func NewDrainer(c *relay.Client, h JobHandler) *FakeIOSDrainer {
	return &FakeIOSDrainer{Client: c, Handler: h}
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
	job, err := jobs.DecodeJob(jb.Blob)
	if err != nil {
		return fmt.Errorf("drainer: decode job %s: %w", jb.JobID, err)
	}
	res, err := d.Handler(ctx, job)
	if err != nil {
		// Wrap the handler error as a structured failed result so the CLI
		// side sees the same shape it would in production.
		res = &health.Result{
			JobID:     job.ID,
			PageIndex: 0,
			Status:    health.StatusFailed,
			Error: &health.JobError{
				Code:    "handler_error",
				Message: err.Error(),
			},
		}
	}
	if res.JobID == "" {
		res.JobID = job.ID
	}
	blob, err := jobs.EncodeResult(res)
	if err != nil {
		return fmt.Errorf("drainer: encode result: %w", err)
	}
	if _, err := d.Client.PostResult(ctx, job.ID, res.PageIndex, blob); err != nil {
		return fmt.Errorf("drainer: post result: %w", err)
	}
	return nil
}
