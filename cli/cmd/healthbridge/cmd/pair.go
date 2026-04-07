package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/pairing"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// pairLinkEmittedHook is called from runPair as soon as the QR has been
// rendered. Tests substitute it with a channel send so they can drive
// the iOS responder side from the same process. Production callers leave
// it nil.
var pairLinkEmittedHook func(*pairing.PairLink)

// defaultPairWait is how long the CLI long-polls for the iOS responder
// after rendering the QR. Generous because the user has to physically
// open the app and aim a camera.
const defaultPairWait = 2 * time.Minute

func newPairCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pair",
		Short: "Pair this CLI with a HealthBridge iPhone",
		Long: `Mints a pair_id, runs the X25519 key exchange against the relay, and
shows a QR code in the terminal. Open HealthBridge on your iPhone, tap
"Pair", and scan the QR. Both the CLI and the iPhone will then show a
six-digit SAS — confirm they match on both sides before approving.

The session key + auth token are saved to ~/.config/healthbridge/pairs/
on success.

Examples:
  healthbridge pair
  healthbridge pair --relay https://healthbridge.example.workers.dev
  healthbridge pair --wait 5m
`,
		RunE: runPair,
	}
	c.Flags().Bool("yes", false, "Skip the SAS confirmation prompt (testing only)")
	return c
}

func runPair(c *cobra.Command, _ []string) error {
	relayURL, _ := c.Flags().GetString("relay")
	if relayURL == "" {
		return fmt.Errorf("--relay is required (or set HEALTHBRIDGE_RELAY)")
	}
	wait, _ := c.Flags().GetDuration("wait")
	if wait <= 0 {
		wait = defaultPairWait
	}

	pairID, err := newPairID()
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}

	rc := relay.New(relayURL, pairID)
	ctx, cancel := withCancellableContext()
	defer cancel()

	out := c.OutOrStdout()
	fmt.Fprintf(out, "Pair ID: %s\nRelay  : %s\n\n", pairID, relayURL)

	partial, link, err := pairing.InitiatePairing(ctx, rc, relayURL)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}

	linkJSON, err := pairing.EncodeLink(link)
	if err != nil {
		return fmt.Errorf("pair: encode link: %w", err)
	}

	if err := renderQR(out, linkJSON); err != nil {
		return fmt.Errorf("pair: render QR: %w", err)
	}
	fmt.Fprintf(out, "\nOpen HealthBridge on your iPhone → Pair → Scan, and point the camera at this QR.\nWaiting up to %s for the phone…\n\n", wait.Round(time.Second))

	if pairLinkEmittedHook != nil {
		pairLinkEmittedHook(link)
	}

	result, err := pairing.CompletePairing(ctx, rc, partial, int(wait/time.Millisecond))
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}

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
		AuthToken:  result.AuthToken,
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

// newPairID returns a fresh ULID for a pairing session.
func newPairID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// renderQR draws the QR code to out using a low-error-correction level
// (which keeps the QR small enough to fit in a typical terminal) and a
// quiet zone of 1 cell on each side.
func renderQR(out io.Writer, payload string) error {
	cfg := qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     out,
		HalfBlocks: true,
		BlackChar:  qrterminal.BLACK_BLACK,
		WhiteChar:  qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		QuietZone:  1,
	}
	qrterminal.GenerateWithConfig(payload, cfg)
	return nil
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
