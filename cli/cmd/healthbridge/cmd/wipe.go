package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
)

func newWipeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "wipe",
		Short: "Delete the local cache, job mirror, and pair record for --pair",
		Long: `Removes everything this CLI knows about the given pair: the SQLite
cache of synced samples, the local job mirror rows, and the pair
record under ~/.config/healthbridge/pairs/. Re-pairing afterwards is
required to use the CLI again.

This does NOT revoke the pair on the relay or on the iPhone. Use a
` + "`DELETE /v1/pair`" + ` directly via curl, or run pair again to overwrite.`,
		RunE: runWipe,
	}
	c.Flags().Bool("yes", false, "Skip the confirmation prompt")
	return c
}

func runWipe(c *cobra.Command, _ []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	yes, _ := c.Flags().GetBool("yes")
	if !yes {
		fmt.Fprintf(c.OutOrStdout(), "This will delete all local state for pair %s. Type 'yes' to continue: ", flags.PairID)
		var line string
		_, _ = fmt.Fscanln(c.InOrStdin(), &line)
		if line != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	if cch, err := openCache(); err == nil {
		_ = cch.Wipe(flags.PairID)
		_ = cch.Close()
	}
	if store, err := openJobStore(); err == nil {
		_ = store.WipePair(flags.PairID)
		_ = store.Close()
	}
	if err := config.DeletePair(configDir(), flags.PairID); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Fprintf(c.OutOrStdout(), "wiped local state for %s\n", flags.PairID)
	return nil
}
