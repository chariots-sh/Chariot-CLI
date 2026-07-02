package cmd

import (
	"fmt"
	"os"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var demoSendToken string

var demoSendCmd = &cobra.Command{
	Use:   "send <agent-id> <message>",
	Short: "Send a message to an agent (as your backend would)",
	Long: `Send a message to an agent, exactly as your backend would:

  POST {api-base}/v1/agents/{agent-id}/messages
  header  X-Chariot-Token: <token-seed>

Authenticates with the token-seed printed once by ` + "`chariot deploy`" + ` — pass it
with --token or set CHARIOT_TOKEN_SEED. Find agent ids with ` + "`chariot list`" + `.
The agent replies asynchronously to the deploy's --endpoint.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentID, message := args[0], args[1]
		token := demoSendToken
		if token == "" {
			token = os.Getenv("CHARIOT_TOKEN_SEED")
		}
		if token == "" {
			return fmt.Errorf("token-seed required — pass --token or set CHARIOT_TOKEN_SEED (printed once by `chariot deploy`)")
		}
		// Token-seed auth, no login needed — config is only read for the base URL.
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		client := api.New(cfg.BaseURL(), "")
		ack, err := client.SendMessage(cmd.Context(), agentID, token, message)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s — agent %s (%s)\n", ack.Status, ack.AgentID, ack.State)
		fmt.Println("  The reply will POST to your deploy --endpoint (see `chariot demo serve`).")
		return nil
	},
}

func init() {
	demoSendCmd.Flags().StringVar(&demoSendToken, "token", "", "token-seed from `chariot deploy` (or CHARIOT_TOKEN_SEED)")
	demoCmd.AddCommand(demoSendCmd)
}
