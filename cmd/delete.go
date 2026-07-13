package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var deleteYes bool

var deleteCmd = &cobra.Command{
	Use:   "delete <agent-id>",
	Short: "Permanently delete an agent",
	Long: `Permanently delete one agent: its pod, PVC, and any other workload
resources are torn down. Unlike hibernation, session state is NOT preserved —
this cannot be undone. If you only want to stop compute while keeping session
state, use ` + "`chariot hibernate <agent>`" + ` instead. Find agent ids with ` + "`chariot list`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentID := args[0]
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if !deleteYes {
			fmt.Fprintf(cmd.OutOrStdout(), "This permanently deletes agent %s and cannot be undone. Continue? [y/N] ", agentID)
			reader := bufio.NewReader(cmd.InOrStdin())
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		if err := client.DeleteAgent(cmd.Context(), agentID); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ deleted agent %s\n", agentID)
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(deleteCmd)
}
