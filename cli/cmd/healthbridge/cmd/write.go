package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newWriteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "write <type>",
		Short: "Write one HealthKit sample",
		Long: `Build a write job for one sample and have the iPhone apply it via
HKHealthStore.save. The agent gets back the HealthKit-assigned UUID
once the iPhone drains the job.

Examples:
  healthbridge write dietary_energy_consumed --value 500 --unit kcal --at now
  healthbridge write body_mass --value 73.2 --unit kg --at 2026-04-07T08:00:00Z
  healthbridge write dietary_water --value 250 --unit mL --meta source=manual

If the iPhone is not currently reachable the call returns
{status: pending, job_id: ...} and the write applies the next time the
HealthBridge app on the phone opens (or whenever a background refresh
happens to drain the queue).`,
		Args: cobra.ExactArgs(1),
		RunE: runWrite,
	}
	c.Flags().Float64("value", 0, "Numeric sample value (required)")
	c.Flags().String("unit", "", "HealthKit unit string, e.g. kcal, kg, mg/dL, mL (required)")
	c.Flags().String("at", "now", "Sample timestamp (RFC3339, YYYY-MM-DD, 'now', or relative offset like -1h)")
	c.Flags().String("end", "", "End timestamp for ranged samples; defaults to --at if omitted")
	c.Flags().StringSlice("meta", nil, "Optional metadata as key=value (repeatable)")
	_ = c.MarkFlagRequired("value")
	_ = c.MarkFlagRequired("unit")
	return c
}

func runWrite(c *cobra.Command, args []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	sampleType := health.SampleType(args[0])
	if !sampleType.IsValid() {
		return fmt.Errorf("unknown sample type %q (try `healthbridge types`)", args[0])
	}

	value, _ := c.Flags().GetFloat64("value")
	unit, _ := c.Flags().GetString("unit")
	atStr, _ := c.Flags().GetString("at")
	endStr, _ := c.Flags().GetString("end")
	metaSlice, _ := c.Flags().GetStringSlice("meta")

	if unit == "" {
		return fmt.Errorf("--unit is required")
	}

	now := time.Now().UTC()
	at, err := parseTimeFlag(atStr, now)
	if err != nil {
		return fmt.Errorf("--at: %w", err)
	}
	endAt := at
	if endStr != "" {
		endAt, err = parseTimeFlag(endStr, now)
		if err != nil {
			return fmt.Errorf("--end: %w", err)
		}
		if endAt.Before(at) {
			return fmt.Errorf("--end must not be before --at")
		}
	}

	meta, err := parseMetaFlags(metaSlice)
	if err != nil {
		return err
	}

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindWrite,
		CreatedAt: now,
		Payload: health.WritePayload{
			Sample: health.Sample{
				Type:     sampleType,
				Value:    value,
				Unit:     unit,
				Start:    at,
				End:      endAt,
				Metadata: meta,
			},
		},
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

	return executeWriteJob(ctx, c.OutOrStdout(), rc, session, store, job, resolveWait(flags), flags.JSON)
}

// parseMetaFlags converts repeated --meta key=value flags into a map.
func parseMetaFlags(items []string) (map[string]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(items))
	for _, kv := range items {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("--meta entry %q must be key=value", kv)
		}
		out[kv[:idx]] = kv[idx+1:]
	}
	return out, nil
}

// executeWriteJob is the body of `write` extracted so scenario tests
// can drive it directly.
//
// Writes are fire-and-forget: we enqueue the job, mirror it locally,
// and emit a `pending` status. We do NOT long-poll for the result
// like read/sync/profile do. The iOS drain ticker (10 s cadence)
// will pick up the job, save it via HKHealthStore.save, and post a
// persistent result. Users who care about the HealthKit-assigned UUID
// can `healthbridge jobs wait <id>` to fetch it after the fact.
//
// Why fire-and-forget? Two reasons:
//
//   1. Long-polling here would force the CLI user to sit through up
//      to 25 seconds of "is it drained yet?" for every write. With
//      the new 10-s tick the iOS app's worst-case drain latency is
//      ~10 s, but the CLI doesn't need to block on it.
//
//   2. Long-polls dominate Cloudflare GB-second billing on the relay.
//      The previous design held a DO open for 25 s on every write
//      result poll, even though the result blob is a single UUID. By
//      not polling here we collapse the per-write DO wall-time from
//      25 s to a couple of milliseconds (just the enqueue).
//
// The persistent flag on the iOS-side result post means the result
// stays in the relay's snapshot until the CLI later acks it via
// `jobs wait` (or until the 24-hour TTL sweep), so nothing is lost.
func executeWriteJob(
	ctx context.Context,
	out io.Writer,
	rc *relay.Client,
	session *jobs.Session,
	store *jobs.Store,
	job *health.Job,
	wait time.Duration,
	asJSON bool,
) error {
	_ = wait // intentionally unused: writes never long-poll
	blob, err := session.SealJob(job)
	if err != nil {
		return fmt.Errorf("seal job: %w", err)
	}
	if err := mirrorEnqueue(store, job, session.PairID); err != nil {
		return fmt.Errorf("mirror enqueue: %w", err)
	}
	if _, err := rc.EnqueueJob(ctx, job.ID, blob); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return emitPending(out, job, asJSON)
}

func emitWriteDone(out io.Writer, job *health.Job, result *health.Result, asJSON bool) error {
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
		return fmt.Errorf("write failed (no error detail)")
	}

	pb, err := json.Marshal(result.Result)
	if err != nil {
		return fmt.Errorf("re-marshal result: %w", err)
	}
	var wr health.WriteResult
	if err := json.Unmarshal(pb, &wr); err != nil {
		return fmt.Errorf("decode write result: %w", err)
	}

	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id": job.ID,
			"status": "done",
			"uuid":   wr.UUID,
		})
	}
	_, err = fmt.Fprintf(out, "wrote sample %s\n", wr.UUID)
	return err
}
