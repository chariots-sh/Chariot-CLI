package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var hibernateCmd = &cobra.Command{
	Use:   "hibernate <agent>",
	Short: "Force-hibernate an agent right now",
	Long: `Force-hibernate an agent immediately, without waiting for its
configured idle window.

A hibernated agent's pod is scaled down (its session state on disk is kept,
not deleted), stopping compute billing until the next message wakes it back
up.

    chariot hibernate my-agent-3

Safe to run on an agent that's already hibernating or was never activated —
both are no-ops. If you want to tear down session state too, use
` + "`chariot delete`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		slug := args[0]
		agent, err := client.HibernateAgent(cmd.Context(), slug)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s\n", agent.Slug, agent.State)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(hibernateCmd)
}
