package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version, Commit, and Date are populated at link time by GoReleaser via
// `-ldflags -X`. When the binary is built without ldflags (e.g. plain
// `go install` or `go build`), they fall back to "dev" / values from
// debug.ReadBuildInfo so the VCS revision still surfaces.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// versionInfo returns the effective version triple, filling in commit
// and date from build metadata when ldflags didn't supply them.
func versionInfo() (version, commit, date string) {
	version = Version
	commit = Commit
	date = Date
	if commit != "" && date != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		}
	}
	return
}

// versionString is the string Cobra prints for `--version`.
func versionString() string {
	v, commit, date := versionInfo()
	if commit == "" && date == "" {
		return fmt.Sprintf("healthbridge %s %s/%s", v, runtime.GOOS, runtime.GOARCH)
	}
	short := commit
	if len(short) > 12 {
		short = short[:12]
	}
	return fmt.Sprintf("healthbridge %s (%s, %s) %s/%s", v, short, date, runtime.GOOS, runtime.GOARCH)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print healthbridge version, commit, and build date",
		RunE: func(c *cobra.Command, _ []string) error {
			flags, _ := commonFromCmdNoPair(c)
			v, commit, date := versionInfo()
			if flags.JSON {
				return writeJSON(c.OutOrStdout(), map[string]any{
					"version": v,
					"commit":  commit,
					"date":    date,
					"go":      runtime.Version(),
					"os":      runtime.GOOS,
					"arch":    runtime.GOARCH,
				})
			}
			_, err := fmt.Fprintln(c.OutOrStdout(), versionString())
			return err
		},
	}
}
