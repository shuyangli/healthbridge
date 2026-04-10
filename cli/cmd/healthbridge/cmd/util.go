package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// commonFlags carries the values of every persistent root flag, parsed
// into typed form. Subcommands call commonFromCmd() to extract them.
type commonFlags struct {
	RelayURL string
	PairID   string
	Wait     time.Duration
	JSON     bool
}

func commonFromCmd(c *cobra.Command) (commonFlags, error) {
	relayURL, _ := c.Flags().GetString("relay")
	pair, _ := c.Flags().GetString("pair")
	wait, _ := c.Flags().GetDuration("wait")
	asJSON, _ := c.Flags().GetBool("json")
	if pair == "" {
		return commonFlags{}, errors.New("--pair is required (run `healthbridge pair`, set HEALTHBRIDGE_PAIR, or save a default in ~/.healthbridge/config)")
	}
	// Pair-record fallback: when nothing on the command line, env, or
	// active default config supplied a relay URL, defaultRelayURL()
	// hands back the localhost dev sentinel. The pair record itself
	// stores the URL the pairing flow used, so prefer that over the
	// localhost fallback. This is what makes `healthbridge read X` work
	// after `healthbridge pair` even on a fresh machine that never had
	// ~/.healthbridge/config written.
	if relayURL == localDevRelayURL && !c.Flags().Changed("relay") {
		if rec, err := config.LoadPair(configDir(), pair); err == nil && rec.RelayURL != "" {
			relayURL = rec.RelayURL
		}
	}
	return commonFlags{
		RelayURL: relayURL,
		PairID:   pair,
		Wait:     wait,
		JSON:     asJSON,
	}, nil
}

// newRelayClient builds a relay.Client from the parsed flags. Stays here
// so subcommands don't need to import the relay package directly. The
// returned client is unauthenticated; use WithAuthToken to attach the
// per-pair token after loading the session.
func newRelayClient(f commonFlags) *relay.Client {
	return relay.New(f.RelayURL, f.PairID)
}

// resolveWait returns the total wall-clock time the CLI will spend
// polling for a result, honouring three sources in priority order:
//  1. --wait flag (if non-zero)
//  2. interactive default (60s — enough for a push to land)
//  3. non-TTY default (0s = fire-and-forget)
func resolveWait(f commonFlags) time.Duration {
	if f.Wait > 0 {
		return f.Wait
	}
	if isStdoutTerminal() {
		return 60 * time.Second
	}
	return 0
}

// pollWindowMs is how long each individual poll request blocks on the
// relay before returning empty. Short enough that the CLI re-evaluates
// frequently (nudge message, total deadline) without holding a
// connection open for a long time.
var pollWindowMs = 3_000

// nudgeDelay is how long to wait before printing the "open the app"
// hint. Exported as a var so tests can shorten it.
var nudgeDelay = 10 * time.Second

// pollWithNudge polls for results in a retry loop using short
// relay-side long-polls (pollWindowMs each). After nudgeDelay without
// a result it prints a user-facing hint to stderr. Returns the first
// non-empty result page, or an empty page if totalWait is exhausted.
func pollWithNudge(
	ctx context.Context,
	rc *relay.Client,
	jobID string,
	totalWait time.Duration,
	stderr io.Writer,
) (*relay.ResultsResponse, error) {
	start := time.Now()
	deadline := start.Add(totalWait)
	nudged := false

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			// Budget exhausted — one final non-blocking check.
			return rc.PollResults(ctx, jobID, 0)
		}

		windowMs := pollWindowMs
		if int(remaining.Milliseconds()) < windowMs {
			windowMs = int(remaining.Milliseconds())
		}

		resp, err := rc.PollResults(ctx, jobID, windowMs)
		if err != nil {
			return nil, err
		}
		if len(resp.Results) > 0 {
			return resp, nil
		}

		if !nudged && time.Since(start) >= nudgeDelay {
			fmt.Fprintln(stderr, "Waiting for iPhone… open the HealthBridge app to speed this up.")
			nudged = true
		}
	}
}

func isStdoutTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func envOrEmpty(name string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return ""
}

// withCancellableContext gives subcommands a context that survives Ctrl-C
// gracefully — long-poll calls return context.Canceled instead of leaving
// connections dangling.
func withCancellableContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// ackResult tells the relay we're done with a job's result pages so
// it can prune them now instead of waiting for the 24h TTL eviction.
// Best-effort: any failure (network, relay down, the entries already
// gone) is silently swallowed because the relay's TTL eviction will
// catch up regardless. Uses its own short timeout so a slow relay
// can't block the user's terminal after a successful read.
func ackResult(parent context.Context, rc *relay.Client, jobID string) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	_, _ = rc.PruneResults(ctx, jobID)
}
