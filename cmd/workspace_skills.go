package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var workspaceSkillsCmd = &cobra.Command{
	Use:   "skills <workspace>",
	Short: "Show which members hold which skills",
	Long: `Show the workspace's skills matrix: what each member holds, and who is
missing each skill.

Grant or revoke across the whole workspace with ` + "`chariot workspace skills add`" + `
and ` + "`remove`" + `; a single agent's grants are handled by ` + "`chariot skills`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		matrix, err := client.GetWorkspaceSkills(cmd.Context(), id)
		if err != nil {
			return err
		}
		printWorkspaceSkills(cmd, matrix)
		return nil
	},
}

var workspaceSkillsAddCmd = &cobra.Command{
	Use:   "add <workspace> <skill>",
	Short: "Grant one skill to every member",
	Long: `Grant a skill to every member of the workspace (idempotent). A running
agent picks the tool up within about a minute, no restart.

` + "`chariot workspace skills catalog`" + ` lists what can be granted.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		matrix, err := client.EnableWorkspaceSkill(cmd.Context(), id, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ granted %s to every member of %s\n\n", args[1], matrix.Name)
		printWorkspaceSkills(cmd, matrix)
		return nil
	},
}

var workspaceSkillsRemoveCmd = &cobra.Command{
	Use:   "remove <workspace> <skill>",
	Short: "Revoke one skill from every member",
	Long: `Revoke a skill from every member of the workspace (idempotent). Scoped to
these members — an agent's grant from somewhere else is untouched.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		matrix, err := client.DisableWorkspaceSkill(cmd.Context(), id, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ revoked %s from every member of %s\n\n", args[1], matrix.Name)
		printWorkspaceSkills(cmd, matrix)
		return nil
	},
}

var workspaceSkillsCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List every skill you can grant",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		skills, err := client.SkillCatalog(cmd.Context())
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SKILL\tWHAT IT GIVES THE AGENT")
		for _, s := range skills {
			fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Description)
		}
		return tw.Flush()
	},
}

func printWorkspaceSkills(cmd *cobra.Command, matrix *api.WorkspaceSkills) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "workspace %s\n", matrix.Name)
	if len(matrix.Agents) == 0 {
		fmt.Fprintln(out, "  no members yet")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  AGENT\tSTATE\tSKILLS")
	for _, a := range matrix.Agents {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", agentLabel(a.Slug, a.Name), a.State, skillsOrNone(a.Skills))
	}
	_ = tw.Flush()

	fmt.Fprintln(out, "\ncoverage")
	cov := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(cov, "  SKILL\tHELD BY\tMISSING")
	for _, c := range matrix.Coverage {
		missing := "-"
		if len(c.Missing) > 0 {
			missing = strings.Join(c.Missing, ", ")
		}
		fmt.Fprintf(cov, "  %s\t%d/%d\t%s\n", c.Name, c.Holders, c.Total, missing)
	}
	_ = cov.Flush()
}

func init() {
	workspaceSkillsCmd.AddCommand(workspaceSkillsAddCmd)
	workspaceSkillsCmd.AddCommand(workspaceSkillsRemoveCmd)
	workspaceSkillsCmd.AddCommand(workspaceSkillsCatalogCmd)
	workspaceCmd.AddCommand(workspaceSkillsCmd)
}
