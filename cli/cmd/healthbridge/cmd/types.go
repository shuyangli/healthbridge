package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func newTypesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "types",
		Short: "List the HealthKit sample types this CLI supports",
		Long: `Prints the stable enum names the CLI uses to identify HealthKit
sample types, along with the unit string each one expects on writes.

The agent skill at skill/healthbridge/SKILL.md depends on this list — if
you ever add a new type to cli/internal/health/types.go, the skill
manifest needs to grow accordingly.`,
		RunE: runTypes,
	}
	return c
}

// canonicalUnitForType returns the unit string the iOS app most commonly
// writes for each sample type. For HKQuantityTypeIdentifier-backed
// types this comes straight from the catalog; for the non-quantity
// carryover (sleep_analysis, workout) the unit is "s" because both are
// reported as durations.
func canonicalUnitForType(t health.SampleType) string {
	if d := health.LookupByWire(t); d != nil {
		return d.Unit
	}
	switch t {
	case health.SleepAnalysis, health.Workout:
		return "s"
	default:
		return ""
	}
}

func runTypes(c *cobra.Command, _ []string) error {
	flags, _ := commonFromCmdNoPair(c) // doesn't require --pair
	out := c.OutOrStdout()
	if flags.JSON {
		entries := make([]map[string]string, 0, len(health.AllSampleTypes()))
		for _, t := range health.AllSampleTypes() {
			entries = append(entries, map[string]string{
				"type": string(t),
				"unit": canonicalUnitForType(t),
			})
		}
		return writeJSON(out, map[string]any{"types": entries})
	}
	fmt.Fprintln(out, "Supported HealthKit sample types:")
	fmt.Fprintln(out)
	for _, t := range health.AllSampleTypes() {
		fmt.Fprintf(out, "  %-26s %s\n", t, canonicalUnitForType(t))
	}
	return nil
}

// commonFromCmdNoPair is the same as commonFromCmd minus the --pair check.
// Used by `healthbridge types` which doesn't need a pair to run.
func commonFromCmdNoPair(c *cobra.Command) (commonFlags, error) {
	relayURL, _ := c.Flags().GetString("relay")
	pair, _ := c.Flags().GetString("pair")
	wait, _ := c.Flags().GetDuration("wait")
	asJSON, _ := c.Flags().GetBool("json")
	return commonFlags{
		RelayURL: relayURL,
		PairID:   pair,
		Wait:     wait,
		JSON:     asJSON,
	}, nil
}
