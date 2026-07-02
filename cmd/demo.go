package cmd

import (
	"github.com/spf13/cobra"
)

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "One-off smoke test of the message round-trip (not a production interface)",
	Long: `One-off smoke test: try the message round-trip from a terminal before you
write any code.

NOT a production interface. Do not script or wrap these commands to build an
application (e.g. a chat service driving the CLI as a subprocess). In
production your own service calls the HTTP API directly:

  POST /v1/agents/{agent-id}/messages     # send a message
  GET  /v1/replies                        # poll replies (or receive webhooks)

Run ` + "`chariot api`" + ` for the full request/response reference, or see
https://app.chariots.sh/docs.

These commands stand in for your service so you can try the loop once:

  chariot demo send <agent-id> "hello"    # message an agent (needs the token-seed)
  chariot demo watch                      # poll the reply inbox — no webhook needed
  chariot demo serve                      # webhook receiver (needs a public tunnel)

The no-tunnel flow: deploy without --endpoint, send, then watch.`,
}

func init() {
	rootCmd.AddCommand(demoCmd)
}
