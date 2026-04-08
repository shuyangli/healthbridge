package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPruneCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "prune <job_id>",
		Short: "Drop a job (and its result pages) from the relay's per-pair mailbox",
		Long: `Tells the relay to forget a single inbound job and any result
pages associated with it. Use this when a job is wedged — most often
when the iOS app's would-be result blob exceeds the relay's
MAX_BLOB_BYTES cap, leaving the iOS drainer in a permanent retry
loop on the same job.

This is a relay-side operation distinct from ` + "`healthbridge jobs prune`" + `,
which only purges the local SQLite job mirror by age. The two are
complementary: ` + "`prune <job_id>`" + ` unwedges the relay; ` + "`jobs prune`" + `
keeps the local mirror tidy.

Pass --results-only to drop only the result pages and leave the
inbound job in place — this is useful if you want the iOS app to
retry executing the job from scratch.

Examples:
  healthbridge prune 6e035d0b61869429493ad4d5d3d099fd
  healthbridge prune 6e035d0b61869429493ad4d5d3d099fd --results-only --json
`,
		Args: cobra.ExactArgs(1),
		RunE: runPrune,
	}
	c.Flags().Bool("results-only", false, "Only drop result pages; leave the inbound job in place")
	return c
}

func runPrune(c *cobra.Command, args []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	jobID := args[0]
	resultsOnly, _ := c.Flags().GetBool("results-only")

	_, authToken, err := loadSession(flags)
	if err != nil {
		return err
	}
	rc := newRelayClient(flags).WithAuthToken(authToken)

	ctx, cancel := withCancellableContext()
	defer cancel()

	if resultsOnly {
		ack, err := rc.PruneResults(ctx, jobID)
		if err != nil {
			return fmt.Errorf("prune results: %w", err)
		}
		return emitPruneAck(c, jobID, "results", ack.Removed, flags.JSON)
	}
	ack, err := rc.PruneJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("prune job: %w", err)
	}
	return emitPruneAck(c, jobID, "job", ack.Removed, flags.JSON)
}

func emitPruneAck(c *cobra.Command, jobID, kind string, removed, asJSON bool) error {
	out := c.OutOrStdout()
	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id":  jobID,
			"kind":    kind,
			"removed": removed,
		})
	}
	if removed {
		_, err := fmt.Fprintf(out, "pruned %s for %s\n", kind, jobID)
		return err
	}
	_, err := fmt.Fprintf(out, "no matching %s for %s\n", kind, jobID)
	return err
}
