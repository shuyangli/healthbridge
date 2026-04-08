package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newJobsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "jobs",
		Short: "Inspect the local job mirror",
		Long: `The local job mirror is a SQLite file that records every job this
CLI has enqueued, plus its final result. Use this surface to follow up
on jobs that returned status=pending — for example after the agent told
you "I've queued the write", you can come back later and check whether
it actually applied.`,
	}
	c.AddCommand(newJobsListCmd())
	c.AddCommand(newJobsGetCmd())
	c.AddCommand(newJobsWaitCmd())
	c.AddCommand(newJobsCancelCmd())
	c.AddCommand(newJobsPruneCmd())
	return c
}

func openJobStore() (*jobs.Store, error) {
	path := jobsDBPath()
	return jobs.Open(path)
}

func jobsDBPath() string {
	if v := os.Getenv("HEALTHBRIDGE_JOBS_DB"); v != "" {
		return v
	}
	dir := configDir()
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		dir = filepath.Join(v, "healthbridge")
	}
	return filepath.Join(dir, "jobs.db")
}

func newJobsListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "Show recent jobs",
		RunE: func(c *cobra.Command, _ []string) error {
			store, err := openJobStore()
			if err != nil {
				return err
			}
			defer store.Close()

			status, _ := c.Flags().GetString("status")
			since, _ := c.Flags().GetDuration("since")
			limit, _ := c.Flags().GetInt("limit")

			f := jobs.ListFilter{
				Status: jobs.JobStatus(status),
				Limit:  limit,
			}
			if since > 0 {
				f.Since = time.Now().Add(-since).UTC()
			}
			recs, err := store.List(f)
			if err != nil {
				return err
			}
			asJSON, _ := c.Flags().GetBool("json")
			return printJobs(c.OutOrStdout(), recs, asJSON)
		},
	}
	c.Flags().String("status", "", "Filter by status (pending, done, failed, expired, canceled)")
	c.Flags().Duration("since", 0, "Only show jobs created within this duration ago, e.g. 24h")
	c.Flags().Int("limit", 50, "Cap the number of rows returned (0 = unlimited)")
	return c
}

func newJobsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one job by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			store, err := openJobStore()
			if err != nil {
				return err
			}
			defer store.Close()
			rec, err := store.Get(args[0])
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("no job with id %s", args[0])
				}
				return err
			}
			asJSON, _ := c.Flags().GetBool("json")
			return printJob(c.OutOrStdout(), rec, asJSON)
		},
	}
}

func newJobsWaitCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "wait <id>",
		Short: "Block until a pending job reaches a terminal state",
		Long: `Wait long-polls the relay for the job's result. If the job is already
in a terminal state in the local mirror, returns immediately. Otherwise
it polls the relay (up to --timeout) and writes the decoded result back
into the mirror.`,
		Args: cobra.ExactArgs(1),
		RunE: runJobsWait,
	}
	c.Flags().Duration("timeout", 60*time.Second, "How long to long-poll the relay")
	return c
}

func runJobsWait(c *cobra.Command, args []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	timeout, _ := c.Flags().GetDuration("timeout")
	store, err := openJobStore()
	if err != nil {
		return err
	}
	defer store.Close()

	asJSON := flags.JSON
	rec, err := store.Get(args[0])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no job with id %s", args[0])
		}
		return err
	}
	if rec.Status != jobs.StatusPending && rec.Status != jobs.StatusRunning {
		return printJob(c.OutOrStdout(), rec, asJSON)
	}

	session, authToken, err := loadSession(flags)
	if err != nil {
		return err
	}
	rc := relay.New(flags.RelayURL, flags.PairID).WithAuthToken(authToken)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := rc.PollResults(ctx, rec.ID, int(timeout/time.Millisecond))
	if err != nil {
		return fmt.Errorf("poll results: %w", err)
	}
	if len(resp.Results) == 0 {
		return printJob(c.OutOrStdout(), rec, asJSON) // still pending
	}
	first := resp.Results[0]
	result, err := session.OpenResult(first.JobID, first.PageIndex, first.Blob)
	if err != nil {
		return fmt.Errorf("open result: %w", err)
	}
	// Ack so the relay prunes the result now instead of holding it
	// until TTL eviction.
	ackResult(ctx, rc, rec.ID)
	if result.Status == "failed" {
		code, msg := "unknown", ""
		if result.Error != nil {
			code = result.Error.Code
			msg = result.Error.Message
		}
		_ = store.MarkFailed(rec.ID, code, msg)
	} else {
		// Re-marshal the typed result back to JSON for storage.
		blob, _ := jsonMarshal(result.Result)
		_ = store.MarkDone(rec.ID, blob)
	}
	updated, _ := store.Get(rec.ID)
	return printJob(c.OutOrStdout(), updated, asJSON)
}

func newJobsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Mark a pending job as canceled in the local mirror",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			store, err := openJobStore()
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.Cancel(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "canceled %s\n", args[0])
			return nil
		},
	}
}

func newJobsPruneCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "prune",
		Short: "Delete terminal-state jobs older than --age",
		RunE: func(c *cobra.Command, _ []string) error {
			age, _ := c.Flags().GetDuration("age")
			store, err := openJobStore()
			if err != nil {
				return err
			}
			defer store.Close()
			n, err := store.Prune(time.Now().Add(-age))
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "pruned %d jobs\n", n)
			return nil
		},
	}
	c.Flags().Duration("age", 30*24*time.Hour, "Delete done/failed/expired/canceled jobs older than this")
	return c
}

// ---- output formatting ----

// jobJSON is the public JSON shape for a job mirror record. Field names
// match the snake_case used elsewhere in the CLI's --json output.
type jobJSON struct {
	ID           string          `json:"id"`
	PairID       string          `json:"pair_id"`
	Kind         string          `json:"kind"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	Deadline     *time.Time      `json:"deadline,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	Attempts     int             `json:"attempts,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}

func jobToJSON(r *jobs.JobRecord) jobJSON {
	j := jobJSON{
		ID:           r.ID,
		PairID:       r.PairID,
		Kind:         r.Kind,
		Status:       string(r.Status),
		CreatedAt:    r.CreatedAt.UTC(),
		Attempts:     r.Attempts,
		ErrorCode:    r.ErrorCode,
		ErrorMessage: r.ErrorMessage,
	}
	if !r.Deadline.IsZero() {
		t := r.Deadline.UTC()
		j.Deadline = &t
	}
	if !r.CompletedAt.IsZero() {
		t := r.CompletedAt.UTC()
		j.CompletedAt = &t
	}
	if len(r.PayloadJSON) > 0 {
		j.Payload = json.RawMessage(r.PayloadJSON)
	}
	if len(r.ResultJSON) > 0 {
		j.Result = json.RawMessage(r.ResultJSON)
	}
	return j
}

func printJobs(out io.Writer, recs []*jobs.JobRecord, asJSON bool) error {
	// Stable order: newest first.
	sort.Slice(recs, func(i, j int) bool { return recs[i].CreatedAt.After(recs[j].CreatedAt) })
	if asJSON {
		arr := make([]jobJSON, 0, len(recs))
		for _, r := range recs {
			arr = append(arr, jobToJSON(r))
		}
		return writeJSON(out, map[string]any{"jobs": arr})
	}
	if len(recs) == 0 {
		fmt.Fprintln(out, "(no jobs)")
		return nil
	}
	fmt.Fprintf(out, "%-32s %-8s %-10s %s\n", "ID", "KIND", "STATUS", "CREATED")
	for _, r := range recs {
		fmt.Fprintf(out, "%-32s %-8s %-10s %s\n",
			r.ID, r.Kind, r.Status, r.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func printJob(out io.Writer, r *jobs.JobRecord, asJSON bool) error {
	if asJSON {
		return writeJSON(out, jobToJSON(r))
	}
	fmt.Fprintf(out, "id         : %s\n", r.ID)
	fmt.Fprintf(out, "pair_id    : %s\n", r.PairID)
	fmt.Fprintf(out, "kind       : %s\n", r.Kind)
	fmt.Fprintf(out, "status     : %s\n", r.Status)
	fmt.Fprintf(out, "created_at : %s\n", r.CreatedAt.Format(time.RFC3339))
	if !r.Deadline.IsZero() {
		fmt.Fprintf(out, "deadline   : %s\n", r.Deadline.Format(time.RFC3339))
	}
	if !r.CompletedAt.IsZero() {
		fmt.Fprintf(out, "completed  : %s\n", r.CompletedAt.Format(time.RFC3339))
	}
	if r.ErrorCode != "" {
		fmt.Fprintf(out, "error      : %s — %s\n", r.ErrorCode, r.ErrorMessage)
	}
	if len(r.ResultJSON) > 0 {
		fmt.Fprintf(out, "result     : %s\n", string(r.ResultJSON))
	}
	return nil
}

// jsonMarshal is a tiny wrapper so the call site reads naturally.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
