package cmd

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/pairing"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newPairCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pair",
		Short: "Pair this CLI with a HealthBridge iOS app",
		Long: `Reads a pair link (JSON produced by the iOS app and shown as a QR code),
runs the X25519 key exchange against the relay, displays a 6-digit SAS
that the user must confirm matches what the iOS app shows, and (on
success) saves the resulting session key + auth token to the local
config under ~/.config/healthbridge/pairs/.

The link is read from --link, or from stdin, or from a path with --link-file.

Examples:
  healthbridge pair --link '{"v":"healthbridge-pair-v1",...}'
  echo '<link json>' | healthbridge pair
  healthbridge pair --link-file ./link.json
`,
		RunE: runPair,
	}
	c.Flags().String("link", "", "Pair link JSON (from the iOS app QR code)")
	c.Flags().String("link-file", "", "Read the pair link JSON from this file")
	c.Flags().Bool("yes", false, "Skip the SAS confirmation prompt (testing only)")
	return c
}

func runPair(c *cobra.Command, _ []string) error {
	link, err := readPairLink(c)
	if err != nil {
		return err
	}
	rc := relay.New(link.RelayURL, link.PairID)

	ctx, cancel := withCancellableContext()
	defer cancel()

	result, err := pairing.PairAsCLI(ctx, rc, link)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}

	out := c.OutOrStdout()
	if err := emitPairSAS(out, result); err != nil {
		return err
	}

	skipPrompt, _ := c.Flags().GetBool("yes")
	if !skipPrompt {
		ok, err := confirmSAS(c.InOrStdin(), out)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("pair aborted: SAS not confirmed")
		}
	}

	rec := &config.PairRecord{
		PairID:     result.PairID,
		RelayURL:   result.RelayURL,
		SessionKey: result.SessionKey,
		IOSPubHex:  hex.EncodeToString(result.IOSPub),
		CLIPubHex:  hex.EncodeToString(result.CLIPub),
		CLIPrivHex: hex.EncodeToString(result.CLIPriv),
	}
	if err := config.SavePair(configDir(), rec); err != nil {
		return fmt.Errorf("save pair: %w", err)
	}
	fmt.Fprintf(out, "\npaired successfully — pair_id=%s\nsession saved under %s\n", result.PairID, configDir())
	return nil
}

func readPairLink(c *cobra.Command) (*pairing.PairLink, error) {
	if path, _ := c.Flags().GetString("link-file"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read link file: %w", err)
		}
		return pairing.DecodeLink(strings.TrimSpace(string(raw)))
	}
	if literal, _ := c.Flags().GetString("link"); literal != "" {
		return pairing.DecodeLink(strings.TrimSpace(literal))
	}
	// Fall back to stdin.
	raw, err := io.ReadAll(c.InOrStdin())
	if err != nil {
		return nil, fmt.Errorf("read link from stdin: %w", err)
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, fmt.Errorf("no pair link provided (--link, --link-file, or stdin required)")
	}
	return pairing.DecodeLink(s)
}

func emitPairSAS(out io.Writer, r *pairing.Result) error {
	_, err := fmt.Fprintf(out, "Pairing complete on the relay.\n\n  pair_id : %s\n  SAS     : %s\n\nConfirm that the iPhone shows the same six-digit code.\n",
		r.PairID, r.SAS)
	return err
}

func confirmSAS(stdin io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Type 'yes' if the codes match: ")
	var line string
	if _, err := fmt.Fscanln(stdin, &line); err != nil && err.Error() != "unexpected newline" {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}
