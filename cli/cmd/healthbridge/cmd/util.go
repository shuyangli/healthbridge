package cmd

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

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
		return commonFlags{}, errors.New("--pair is required (set HEALTHBRIDGE_PAIR or pass --pair)")
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

// resolveWait returns the wait duration the CLI should pass to long-poll,
// honouring three sources in priority order:
//   1. --wait flag (if non-zero)
//   2. interactive default (5s)
//   3. non-TTY default (0s = fire-and-forget)
func resolveWait(f commonFlags) time.Duration {
	if f.Wait > 0 {
		return f.Wait
	}
	if isStdoutTerminal() {
		return 5 * time.Second
	}
	return 0
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
