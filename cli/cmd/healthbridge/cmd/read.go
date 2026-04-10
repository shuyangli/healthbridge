package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newReadCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "read <type>",
		Short: "Read recent samples for a HealthKit type",
		Long: `Enqueue a read job for a single sample type and wait for the iOS app
to drain it. The agent gets back the matching samples (if the iPhone is
reachable in time) or a job ID it can poll later.

Example:
  healthbridge read step_count --from 2026-04-01 --to 2026-04-07
  healthbridge read heart_rate_resting --from -7d --json
`,
		Args: cobra.ExactArgs(1),
		RunE: runRead,
	}
	c.Flags().String("from", "-1d", "RFC3339 timestamp or relative offset like -1d / -6h")
	c.Flags().String("to", "now", "RFC3339 timestamp or 'now'")
	c.Flags().Int("limit", 0, "Cap on samples returned. 0 = no cap.")
	return c
}

func runRead(c *cobra.Command, args []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}

	sampleType := health.SampleType(args[0])
	if !sampleType.IsValid() {
		return fmt.Errorf("unknown sample type %q (try `healthbridge types`)", args[0])
	}

	now := time.Now().UTC()
	fromStr, _ := c.Flags().GetString("from")
	toStr, _ := c.Flags().GetString("to")
	limit, _ := c.Flags().GetInt("limit")

	from, err := parseTimeFlag(fromStr, now)
	if err != nil {
		return fmt.Errorf("--from: %w", err)
	}
	to, err := parseTimeFlag(toStr, now)
	if err != nil {
		return fmt.Errorf("--to: %w", err)
	}
	if !to.After(from) {
		return fmt.Errorf("--to must be after --from")
	}

	job := jobs.NewReadJob(sampleType, from, to)
	if limit > 0 {
		// re-build payload to attach limit
		job.Payload = health.ReadPayload{Type: sampleType, From: from, To: to, Limit: limit}
	}

	session, authToken, err := loadSession(flags)
	if err != nil {
		return err
	}
	rc := newRelayClient(flags).WithAuthToken(authToken)
	store, err := openJobStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := withCancellableContext()
	defer cancel()

	return executeReadJob(ctx, c.OutOrStdout(), rc, session, store, job, resolveWait(flags), flags.JSON)
}

// executeReadJob is the body of `read` extracted so scenario tests can drive
// it directly with their own context, writer, relay client, and session.
//
// The local job mirror is updated alongside the relay enqueue/poll: this
// is what `healthbridge jobs list/get/wait` reads. The mirror is optional
// (a nil store is treated as a no-op) so unit tests that don't care about
// persistence can pass nil.
func executeReadJob(
	ctx context.Context,
	out io.Writer,
	rc *relay.Client,
	session *jobs.Session,
	store *jobs.Store,
	job *health.Job,
	wait time.Duration,
	asJSON bool,
) error {
	blob, err := session.SealJob(job)
	if err != nil {
		return fmt.Errorf("seal job: %w", err)
	}
	if err := mirrorEnqueue(store, job, session.PairID); err != nil {
		return fmt.Errorf("mirror enqueue: %w", err)
	}
	if _, err := rc.EnqueueJob(ctx, job.ID, blob, "alert"); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	resp, err := pollWithNudge(ctx, rc, job.ID, wait, os.Stderr)
	if err != nil {
		return fmt.Errorf("poll results: %w", err)
	}

	if len(resp.Results) == 0 {
		return emitPending(out, job, asJSON)
	}

	// M1: read jobs always produce one result page.
	first := resp.Results[0]
	result, err := session.OpenResult(first.JobID, first.PageIndex, first.Blob)
	if err != nil {
		return fmt.Errorf("open result: %w", err)
	}
	mirrorComplete(store, job.ID, result)
	// Ack to the relay so it prunes the (ephemeral) result page now
	// instead of letting it sit in memory until the alarm or TTL
	// sweep. Best-effort.
	ackResult(ctx, rc, job.ID)
	return emitDone(out, job, result, asJSON)
}

func emitPending(out io.Writer, job *health.Job, asJSON bool) error {
	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id": job.ID,
			"status": "pending",
		})
	}
	_, err := fmt.Fprintf(out, "pending: job %s queued; iPhone hasn't drained it yet\n", job.ID)
	return err
}

func emitDone(out io.Writer, job *health.Job, result *health.Result, asJSON bool) error {
	if result.Status == health.StatusFailed {
		if asJSON {
			return writeJSON(out, map[string]any{
				"job_id": job.ID,
				"status": "failed",
				"error":  result.Error,
			})
		}
		if result.Error != nil {
			return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
		}
		return fmt.Errorf("read failed (no error detail)")
	}

	// Decode the typed result payload from the generic any{}.
	pb, err := json.Marshal(result.Result)
	if err != nil {
		return fmt.Errorf("re-marshal result: %w", err)
	}
	var rr health.ReadResult
	if err := json.Unmarshal(pb, &rr); err != nil {
		return fmt.Errorf("decode read result: %w", err)
	}

	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id":  job.ID,
			"status":  "done",
			"type":    rr.Type,
			"samples": rr.Samples,
		})
	}
	if len(rr.Samples) == 0 {
		_, err := fmt.Fprintf(out, "no samples for %s\n", rr.Type)
		return err
	}
	for _, s := range rr.Samples {
		_, err := fmt.Fprintf(out, "%s\t%v %s\t%s..%s\n",
			s.Type, s.Value, s.Unit, s.Start.Format(time.RFC3339), s.End.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
