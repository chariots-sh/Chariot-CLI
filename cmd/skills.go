package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
)

var skillsAgent string

var skillsCmd = &cobra.Command{
	Use:   "skills --agent <agent-id>",
	Short: "Show or change an agent's skills (extra tools, e.g. docs)",
	Long: `Show one agent's skills — extra tools granted beyond the basics.

Available skills:

  docs   shared documents between your agents — each agent can read every
         document, add its own, and append to the others'; you manage the
         document spaces and their membership in the web app.

"granted" lists what you gave this agent explicitly (` + "`chariot deploy --skills`" + `
or ` + "`chariot skills add`" + `); "effective" is what the agent actually has, which
also includes skills implied elsewhere (docs, for agents in a shared-documents
space). Grant with ` + "`chariot skills add`" + `, revoke with ` + "`chariot skills remove`" + ` —
a running agent picks changes up within about a minute, no restart.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if skillsAgent == "" {
			return fmt.Errorf("--agent is required (find ids with `chariot list`)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		state, err := client.GetAgentSkills(cmd.Context(), skillsAgent)
		if err != nil {
			return err
		}
		printSkills(cmd, skillsAgent, state)
		return nil
	},
}

var skillsAddCmd = &cobra.Command{
	Use:   "add <skill> --agent <agent-id>",
	Short: "Grant a skill to an existing agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if skillsAgent == "" {
			return fmt.Errorf("--agent is required (find ids with `chariot list`)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		state, err := client.AddAgentSkill(cmd.Context(), skillsAgent, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ granted %s\n", args[0])
		printSkills(cmd, skillsAgent, state)
		return nil
	},
}

var skillsRemoveCmd = &cobra.Command{
	Use:   "remove <skill> --agent <agent-id>",
	Short: "Revoke a skill you granted an agent",
	Long: `Revoke a skill you granted explicitly.

A skill the agent also holds through a shared-documents space stays effective
until the agent leaves the space — the printed "effective" line shows what the
agent still has.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if skillsAgent == "" {
			return fmt.Errorf("--agent is required (find ids with `chariot list`)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		state, err := client.RemoveAgentSkill(cmd.Context(), skillsAgent, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ revoked %s\n", args[0])
		printSkills(cmd, skillsAgent, state)
		return nil
	},
}

func printSkills(cmd *cobra.Command, agentRef string, state *api.AgentSkills) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "agent %s\n", agentRef)
	fmt.Fprintf(out, "  granted    : %s\n", skillsOrNone(state.Granted))
	fmt.Fprintf(out, "  effective  : %s\n", skillsOrNone(state.Effective))
}

func skillsOrNone(skills []string) string {
	if len(skills) == 0 {
		return "none"
	}
	return strings.Join(skills, ", ")
}

func init() {
	skillsCmd.PersistentFlags().StringVar(&skillsAgent, "agent", "", "agent id, slug, or name (see `chariot list`)")
	skillsCmd.AddCommand(skillsAddCmd)
	skillsCmd.AddCommand(skillsRemoveCmd)
	rootCmd.AddCommand(skillsCmd)
}
