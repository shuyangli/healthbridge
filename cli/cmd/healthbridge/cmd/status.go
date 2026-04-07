package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newStatusCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Show pair info, scopes, and relay reachability",
		Long: `Reads the local pair record for --pair, prints the relay URL,
granted scopes, and pings the relay's /v1/health endpoint to confirm
it's reachable.

In M4+ this also reports queue depth and last sync timestamps.`,
		RunE: runStatus,
	}
	return c
}

func runStatus(c *cobra.Command, _ []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	rec, err := loadPairRecord(c)
	if err != nil {
		return err
	}

	rc := relay.New(rec.RelayURL, rec.PairID).WithAuthToken(rec.AuthToken)
	ctx, cancel := withCancellableContext()
	defer cancel()

	relayOK := true
	relayErr := ""
	if err := pingRelay(ctx, rc); err != nil {
		relayOK = false
		relayErr = err.Error()
	}

	out := c.OutOrStdout()
	if flags.JSON {
		return writeJSON(out, map[string]any{
			"pair_id":   rec.PairID,
			"relay_url": rec.RelayURL,
			"scopes":    rec.Scopes,
			"created":   rec.CreatedAt,
			"relay_ok":  relayOK,
			"relay_err": relayErr,
		})
	}
	fmt.Fprintf(out, "pair_id  : %s\n", rec.PairID)
	fmt.Fprintf(out, "relay    : %s\n", rec.RelayURL)
	if len(rec.Scopes) == 0 {
		fmt.Fprintln(out, "scopes   : (all sample types)")
	} else {
		fmt.Fprintf(out, "scopes   : %s\n", strings.Join(rec.Scopes, ", "))
	}
	fmt.Fprintf(out, "created  : %s\n", rec.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	if relayOK {
		fmt.Fprintln(out, "relay_ok : yes")
	} else {
		fmt.Fprintf(out, "relay_ok : no — %s\n", relayErr)
	}
	return nil
}

// pingRelay hits /v1/health on the configured relay. Uses a short context.
func pingRelay(ctx context.Context, rc *relay.Client) error {
	// Use a one-off raw HTTP get; the relay client doesn't expose health.
	// We'd add a Health() method here in production but for now this
	// inline call is fine.
	resp, err := rc.HTTP.Get(rc.BaseURL + "/v1/health")
	_ = ctx
	if err != nil {
		return fmt.Errorf("contact relay: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode health: %w", err)
	}
	if !body.OK {
		return fmt.Errorf("relay reports not ok")
	}
	return nil
}
