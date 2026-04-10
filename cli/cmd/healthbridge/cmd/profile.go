package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newProfileCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "profile <field>",
		Short: "Read one HealthKit characteristic (DOB, biological sex, blood type, …)",
		Long: `Fetch a single HealthKit characteristic the user set in the Health
app. Characteristic types are read-once profile data — they have no
time range, value, or unit, so they don't fit the read/write/sync
surface and live behind their own subcommand.

Supported fields:

  date_of_birth          ISO date string (YYYY-MM-DD)
  biological_sex         "female", "male", "other", "not_set"
  blood_type             "a_positive", "b_negative", …, "not_set"
  fitzpatrick_skin_type  "type_i" through "type_vi", "not_set"
  wheelchair_use         "yes", "no", "not_set"
  activity_move_mode     "active_energy", "apple_move_time", "not_set"

The agent uses these to ground fitness-coaching answers — e.g. age
from date_of_birth, basal energy estimates from biological_sex.

Examples:
  healthbridge profile biological_sex --json
  healthbridge profile date_of_birth --json
`,
		Args: cobra.ExactArgs(1),
		RunE: runProfile,
	}
	return c
}

func runProfile(c *cobra.Command, args []string) error {
	// Validate the field first so the user sees a helpful "unknown
	// characteristic" message even when --pair / --relay aren't set.
	field := health.CharacteristicType(args[0])
	if !field.IsValid() {
		return fmt.Errorf(
			"unknown characteristic %q (try one of: %s)",
			args[0],
			joinCharacteristicTypes(health.AllCharacteristicTypes()),
		)
	}
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindProfile,
		CreatedAt: time.Now().UTC(),
		Payload:   health.ProfilePayload{Field: field},
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

	return executeProfileJob(ctx, c.OutOrStdout(), rc, session, store, job, resolveWait(flags), flags.JSON)
}

func joinCharacteristicTypes(cs []health.CharacteristicType) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c)
	}
	return strings.Join(out, ", ")
}

// executeProfileJob is the body of `profile` extracted so scenario
// tests can drive it directly. The shape parallels executeReadJob /
// executeWriteJob — same seal/enqueue/poll pattern, different result
// type.
func executeProfileJob(
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
	if _, err := rc.EnqueueJob(ctx, job.ID, blob); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	resp, err := pollWithNudge(ctx, rc, job.ID, wait, os.Stderr)
	if err != nil {
		return fmt.Errorf("poll results: %w", err)
	}
	if len(resp.Results) == 0 {
		return emitPending(out, job, asJSON)
	}

	first := resp.Results[0]
	result, err := session.OpenResult(first.JobID, first.PageIndex, first.Blob)
	if err != nil {
		return fmt.Errorf("open result: %w", err)
	}
	mirrorComplete(store, job.ID, result)
	// Ack so the relay prunes the (persistent) profile result now
	// rather than holding it for 24h. Best-effort.
	ackResult(ctx, rc, job.ID)
	return emitProfileDone(out, job, result, asJSON)
}

func emitProfileDone(out io.Writer, job *health.Job, result *health.Result, asJSON bool) error {
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
		return fmt.Errorf("profile read failed (no error detail)")
	}

	pb, err := json.Marshal(result.Result)
	if err != nil {
		return fmt.Errorf("re-marshal result: %w", err)
	}
	var pr health.ProfileResult
	if err := json.Unmarshal(pb, &pr); err != nil {
		return fmt.Errorf("decode profile result: %w", err)
	}

	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id": job.ID,
			"status": "done",
			"field":  pr.Field,
			"value":  pr.Value,
		})
	}
	if pr.Value == "" {
		_, err = fmt.Fprintf(out, "%s: (not set in Health app)\n", pr.Field)
		return err
	}
	_, err = fmt.Fprintf(out, "%s: %s\n", pr.Field, pr.Value)
	return err
}
