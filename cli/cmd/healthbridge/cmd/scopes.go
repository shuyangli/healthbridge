package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func newScopesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "scopes",
		Short: "List, grant, or revoke HealthKit data-type scopes for a pair",
		Long: `Scopes are the set of HealthKit sample types this CLI is allowed to
read and write through the iPhone. They are stored in the local pair
record (~/.config/healthbridge/pairs/<pair_id>.json). The iOS app
re-validates every operation against its own copy of the scopes, so
revoking here does not (yet) revoke server-side — that lands when the
consent ledger is wired in M3.

Examples:
  healthbridge scopes list
  healthbridge scopes grant step_count dietary_energy_consumed
  healthbridge scopes revoke dietary_water
`,
	}
	c.AddCommand(newScopesListCmd())
	c.AddCommand(newScopesGrantCmd())
	c.AddCommand(newScopesRevokeCmd())
	return c
}

func newScopesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the granted scopes for the current pair",
		RunE: func(c *cobra.Command, _ []string) error {
			rec, err := loadPairRecord(c)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if len(rec.Scopes) == 0 {
				fmt.Fprintln(out, "all sample types granted (default)")
				for _, t := range health.AllSampleTypes() {
					fmt.Fprintf(out, "  - %s\n", t)
				}
				return nil
			}
			fmt.Fprintln(out, "granted:")
			sort.Strings(rec.Scopes)
			for _, s := range rec.Scopes {
				fmt.Fprintf(out, "  - %s\n", s)
			}
			return nil
		},
	}
}

func newScopesGrantCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <type> [type...]",
		Short: "Add one or more sample types to the pair's scope set",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			rec, err := loadPairRecord(c)
			if err != nil {
				return err
			}
			set := make(map[string]bool, len(rec.Scopes))
			for _, s := range rec.Scopes {
				set[s] = true
			}
			for _, a := range args {
				if !health.SampleType(a).IsValid() {
					return fmt.Errorf("unknown sample type %q (try `healthbridge types`)", a)
				}
				set[a] = true
			}
			rec.Scopes = sortedKeys(set)
			if err := config.SavePair(configDir(), rec); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "granted: %s\n", strings.Join(rec.Scopes, ", "))
			return nil
		},
	}
}

func newScopesRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <type> [type...]",
		Short: "Remove one or more sample types from the pair's scope set",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			rec, err := loadPairRecord(c)
			if err != nil {
				return err
			}
			// An empty list means "everything granted". The first revoke
			// must materialise the full set so the user can subtract from it.
			if len(rec.Scopes) == 0 {
				for _, t := range health.AllSampleTypes() {
					rec.Scopes = append(rec.Scopes, string(t))
				}
			}
			set := make(map[string]bool, len(rec.Scopes))
			for _, s := range rec.Scopes {
				set[s] = true
			}
			for _, a := range args {
				delete(set, a)
			}
			rec.Scopes = sortedKeys(set)
			if err := config.SavePair(configDir(), rec); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "remaining: %s\n", strings.Join(rec.Scopes, ", "))
			return nil
		},
	}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func loadPairRecord(c *cobra.Command) (*config.PairRecord, error) {
	flags, err := commonFromCmd(c)
	if err != nil {
		return nil, err
	}
	rec, err := config.LoadPair(configDir(), flags.PairID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no pair record for %s — run `healthbridge pair` first", flags.PairID)
		}
		return nil, err
	}
	return rec, nil
}
