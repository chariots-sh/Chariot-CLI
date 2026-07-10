package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var fleetSkillFrom string

var fleetSkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage a pack's setup skill (markdown, for whoever deploys it)",
	Long: `Manage a pack's setup skill: a markdown document walking whoever deploys the
pack — a human, or the coding agent ` + "`chariot deploy-fleet`" + ` hands it to — through
wiring the fleet up: what to message first, webhook payload shapes, how the
agents divide the work. It is NOT injected into the agent pods.`,
}

var fleetSkillShowCmd = &cobra.Command{
	Use:   "show <pack>",
	Short: "Print a pack's setup skill",
	Long: `Print a pack's setup skill as raw markdown.

Shows your own pack's skill; with --from, a published pack's — readable before
deploying (it is part of deciding whether to deploy at all).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if fleetSkillFrom != "" {
			skill, err := client.GetPublicFleetSkill(cmd.Context(), fleetSkillFrom, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), skill.Content)
			return nil
		}
		skill, err := client.GetFleetSkill(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), skill.Content)
		return nil
	},
}

var fleetSkillSetCmd = &cobra.Command{
	Use:   "set <pack> <file.md>",
	Short: "Attach (or replace) a pack's setup skill from a markdown file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("reading %s: %w", args[1], err)
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if _, err := client.SetFleetSkill(cmd.Context(), args[0], string(content)); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ setup skill attached to %s\n", args[0])
		fmt.Fprintln(cmd.OutOrStdout(), "  Everyone who deploys the pack gets it — displayed, or handed to their coding agent.")
		return nil
	},
}

var fleetSkillClearCmd = &cobra.Command{
	Use:   "clear <pack>",
	Short: "Remove a pack's setup skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if err := client.ClearFleetSkill(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ setup skill removed from %s\n", args[0])
		return nil
	},
}

func init() {
	fleetSkillShowCmd.Flags().StringVar(&fleetSkillFrom, "from", "",
		"owner email — read a published pack's skill instead of your own")
	fleetSkillCmd.AddCommand(fleetSkillShowCmd)
	fleetSkillCmd.AddCommand(fleetSkillSetCmd)
	fleetSkillCmd.AddCommand(fleetSkillClearCmd)
	fleetCmd.AddCommand(fleetSkillCmd)
}
