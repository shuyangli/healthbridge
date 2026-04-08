package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
)

func newUnpairCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "unpair",
		Short: "Forget a pair locally (pair record + active default)",
		Long: `Removes the pair record under ~/.config/healthbridge/pairs/ and
clears the active default in ~/.healthbridge/config if it points to
this pair. Use this when you have unpaired from the iOS app and want
the CLI to stop trying to talk to a stale session.

The local sample cache and job mirror are NOT touched — use
` + "`healthbridge wipe`" + ` if you want the full nuclear cleanup
(cache + job mirror + pair record).

This command does NOT revoke the pair on the relay. The relay's
per-pair Durable Object stays alive until you call its
DELETE /v1/pair endpoint or it ages out.`,
		RunE: runUnpair,
	}
	return c
}

func runUnpair(c *cobra.Command, _ []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	out := c.OutOrStdout()

	// 1. Remove the pair record file. config.DeletePair swallows
	//    ENOENT, so we stat first to know whether we actually had a
	//    record to remove (the JSON output reports both states).
	pairRecordRemoved := false
	if _, statErr := config.LoadPair(configDir(), flags.PairID); statErr == nil {
		pairRecordRemoved = true
	} else if !os.IsNotExist(statErr) {
		// Record exists but is corrupt — still treat unpair as a
		// "best effort delete" rather than failing the user out.
		pairRecordRemoved = true
	}
	if err := config.DeletePair(configDir(), flags.PairID); err != nil {
		return fmt.Errorf("delete pair record: %w", err)
	}

	// 2. Clear the active default if (and only if) it points to the
	//    pair we are forgetting. We never clobber a default that
	//    targets a different pair.
	defaultCleared := false
	if cfg, cfgErr := loadDefaultConfig(); cfgErr == nil && cfg.PairID == flags.PairID {
		if err := os.Remove(defaultConfigPath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear default config: %w", err)
		}
		defaultCleared = true
	}

	if flags.JSON {
		return writeJSON(out, map[string]any{
			"pair_id":             flags.PairID,
			"pair_record_removed": pairRecordRemoved,
			"default_cleared":     defaultCleared,
		})
	}

	if !pairRecordRemoved && !defaultCleared {
		_, err := fmt.Fprintf(out, "no local state for %s\n", flags.PairID)
		return err
	}
	if pairRecordRemoved {
		fmt.Fprintf(out, "removed pair record for %s\n", flags.PairID)
	}
	if defaultCleared {
		fmt.Fprintf(out, "cleared active default at %s\n", defaultConfigPath())
	}
	return nil
}
