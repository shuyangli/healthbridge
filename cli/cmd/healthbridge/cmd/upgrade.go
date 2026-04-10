package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Print instructions for upgrading healthbridge to the latest version",
		RunE: func(c *cobra.Command, _ []string) error {
			v, _, _ := versionInfo()
			fmt.Fprintf(c.OutOrStdout(), "Current version: %s\n\n", v)
			fmt.Fprintln(c.OutOrStdout(), "To upgrade via Homebrew:")
			fmt.Fprintln(c.OutOrStdout(), "  brew upgrade shuyangli/tap/healthbridge")
			fmt.Fprintln(c.OutOrStdout(), "")
			fmt.Fprintln(c.OutOrStdout(), "If you haven't installed via Homebrew yet:")
			fmt.Fprintln(c.OutOrStdout(), "  brew install shuyangli/tap/healthbridge")
			return nil
		},
	}
}
