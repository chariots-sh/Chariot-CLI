package cmd

import (
	"github.com/spf13/cobra"
)

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Try the message round-trip without writing a backend",
	Long: `Demo helpers for the message round-trip.

In production your backend messages agents and receives their replies. These
commands stand in for that backend so you can try the loop from a terminal:

  chariot demo send <agent-id> "hello"    # message an agent (needs the token-seed)
  chariot demo watch                      # poll the reply inbox — no webhook needed
  chariot demo serve                      # webhook receiver (needs a public tunnel)

The no-tunnel flow: deploy without --endpoint, send, then watch.`,
}

func init() {
	rootCmd.AddCommand(demoCmd)
}
