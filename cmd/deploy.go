package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	deployCount    int
	deployEndpoint string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Create a fleet of agents",
	Long: `Create a fleet of agents.

Agents start deactivated and cost nothing until you message them. Deploy prints
a token-seed (shown once) — your backend uses it, together with an agent id from
` + "`chariot list`" + `, to send messages.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if deployCount <= 0 {
			return fmt.Errorf("--count must be positive")
		}
		if deployEndpoint == "" {
			return fmt.Errorf("--endpoint is required (where your agents POST replies)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		res, err := client.Deploy(cmd.Context(), deployCount, deployEndpoint)
		if err != nil {
			return err
		}
		fmt.Printf("\n✓ %d agents live (total %d)\n\n", res.Created, res.Total)
		fmt.Printf("  endpoint    : %s\n", deployEndpoint)
		fmt.Printf("  namespace   : %s\n", res.Namespace)
		fmt.Printf("  token-seed  : %s\n", res.TokenSeed)
		fmt.Println("\n  Save the token-seed now — it is shown only once.")
		fmt.Println("  Address an agent from your backend:")
		fmt.Println("    POST {endpoint-base}/v1/agents/{agent-id}/messages")
		fmt.Println("    header  X-Chariot-Token: " + res.TokenSeed)
		fmt.Println("  Find agent ids with `chariot list`.")
		return nil
	},
}

func init() {
	deployCmd.Flags().IntVar(&deployCount, "count", 0, "number of agents to create")
	deployCmd.Flags().StringVar(&deployEndpoint, "endpoint", "", "webhook URL your agents reply to")
	rootCmd.AddCommand(deployCmd)
}
