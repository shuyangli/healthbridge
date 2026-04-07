// Package cmd holds the Cobra command tree for the healthbridge CLI. The
// commands here are the agent-facing surface; the heavy lifting (encoding
// jobs, talking to the relay, applying results) lives in cli/internal.
package cmd

import (
	"github.com/spf13/cobra"
)

// Root builds a fresh command tree. Tests construct one per invocation so
// they can wire stdout/stderr buffers; production callers from main() use
// the singleton.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "healthbridge",
		Short:         "Read and write Apple Health data via a serverless relay",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("relay", defaultRelayURL(), "Base URL of the healthbridge relay")
	root.PersistentFlags().String("pair", "", "Pair ID (26-char ULID) to talk to. Required until pairing lands in M2.")
	root.PersistentFlags().Duration("wait", 0, "How long to long-poll for a result before returning pending. 0 = use the default.")
	root.PersistentFlags().Bool("json", false, "Emit machine-readable JSON instead of human output")

	root.AddCommand(newReadCmd())
	root.AddCommand(newWriteCmd())
	root.AddCommand(newPairCmd())
	root.AddCommand(newScopesCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newJobsCmd())
	return root
}

const rootLong = `healthbridge is the desktop CLI for the HealthBridge project.

It encodes a job (read, write, or sync), pushes it to a tiny serverless
relay, and waits for the iOS HealthBridge app to drain it the next time
the user opens the phone. Every job and result that crosses the relay is
end-to-end encrypted (M2+); the relay is a dumb mailbox.

In M1 only ` + "`read`" + ` is supported, and blobs are plaintext base64.`

func defaultRelayURL() string {
	if v := envOrEmpty("HEALTHBRIDGE_RELAY"); v != "" {
		return v
	}
	return "http://127.0.0.1:8787"
}
