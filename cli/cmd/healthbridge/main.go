// healthbridge is the desktop CLI that lets local AI agents read and
// write Apple Health data via a tiny serverless relay and a companion
// iOS app.
//
// In M1 the binary supports a single subcommand (`read`) and the relay
// blob format is plaintext base64. Pairing, encryption, writes, sync, and
// the job-mirror surface arrive in later milestones.
package main

import (
	"fmt"
	"os"

	"github.com/shuyangli/healthbridge/cli/cmd/healthbridge/cmd"
)

func main() {
	if err := cmd.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "healthbridge:", err)
		os.Exit(1)
	}
}
