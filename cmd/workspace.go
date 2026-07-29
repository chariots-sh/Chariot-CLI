package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var workspaceDeleteYes bool

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Group agents into a workspace — chat, shared documents, skills",
	Long: `Group some of your agents into a workspace.

A workspace is a room: message one member, or broadcast to all of them at
once; the members read and write a set of shared documents, and they can hand
work to each other. Joining a workspace equips an agent with the tools for
both (` + "`chariot workspace skills`" + ` shows and changes what they hold).

  chariot workspace create research
  chariot workspace add research agent-000001 agent-000002
  chariot workspace chat research "what are we missing on the Q3 filing?"
  chariot workspace chat research "your turn" --agent agent-000001
  chariot workspace docs research                     # what they wrote down
  chariot workspace crosstalk research                # what they told each other

Workspaces are the same ones the web app shows; a workspace made in either
place is visible in both.`,
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your workspaces",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		spaces, err := client.ListWorkspaces(cmd.Context())
		if err != nil {
			return err
		}
		if len(spaces) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "No workspaces yet — create one with `chariot workspace create <name>`.")
			return nil
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAGENTS\tCREATED\tID")
		for _, ws := range spaces {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", ws.Name, ws.AgentCount, ws.CreatedAt.Local().Format("2006-01-02"), ws.ID)
		}
		return tw.Flush()
	},
}

var workspaceCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an empty workspace",
	Long: `Create an empty workspace, then put agents in it with
` + "`chariot workspace add`" + `. Names are lowercase letters, digits and '-'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ws, err := client.CreateWorkspace(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ created workspace %s (%s)\n", ws.Name, ws.ID)
		fmt.Fprintf(cmd.ErrOrStderr(), "  Add agents: chariot workspace add %s <agent>...\n", ws.Name)
		return nil
	},
}

var workspaceShowCmd = &cobra.Command{
	Use:   "show <workspace>",
	Short: "Show a workspace and its members",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		detail, err := client.GetWorkspace(cmd.Context(), id)
		if err != nil {
			return err
		}
		printWorkspace(cmd, detail)
		return nil
	},
}

var workspaceDeleteCmd = &cobra.Command{
	Use:   "delete <workspace>",
	Short: "Delete a workspace (the agents keep running)",
	Long: `Delete a workspace: its membership, chat threads and shared documents go
with it. The member agents themselves are untouched — they keep running, and
keep the skills they were granted.`,
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
		if !workspaceDeleteYes {
			fmt.Fprintf(cmd.OutOrStdout(), "This deletes workspace %s, its chat threads and its shared documents. Continue? [y/N] ", args[0])
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		if err := client.DeleteWorkspace(cmd.Context(), id); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ deleted workspace %s\n", args[0])
		return nil
	},
}

var workspaceAddCmd = &cobra.Command{
	Use:   "add <workspace> <agent>...",
	Short: "Add agents to a workspace",
	Long: `Add one or more agents (id, slug, or name) to a workspace. Re-adding an
existing member is a no-op.

Joining grants the workspace skills — shared documents and agent-to-agent
messaging — so a new member arrives able to use them. A running agent picks
the tools up within about a minute.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		detail, err := client.AddWorkspaceAgents(cmd.Context(), id, args[1:])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s now has %d member(s)\n\n", detail.Name, len(detail.Agents))
		printWorkspace(cmd, detail)
		return nil
	},
}

var workspaceRemoveCmd = &cobra.Command{
	Use:   "remove <workspace> <agent>",
	Short: "Remove one agent from a workspace",
	Long: `Remove one member. Its chat lines stay in the thread, and the agent keeps
the skills it was granted — revoke those with ` + "`chariot skills remove`" + `.`,
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
		detail, err := client.RemoveWorkspaceAgent(cmd.Context(), id, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ removed %s from %s\n\n", args[1], detail.Name)
		printWorkspace(cmd, detail)
		return nil
	},
}

var workspaceCrosstalkCmd = &cobra.Command{
	Use:   "crosstalk <workspace>",
	Short: "Show what the members have said to each other",
	Long: `Print the workspace's agent-to-agent messages, oldest first — the handoffs
and coordination the members did on their own, which never appear in your chat
thread.`,
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
		lines, err := client.WorkspaceCrosstalk(cmd.Context(), id)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "No agent-to-agent messages yet.")
			return nil
		}
		out := cmd.OutOrStdout()
		for _, line := range lines {
			fmt.Fprintf(out, "[%s] %s → %s", line.CreatedAt.Local().Format("01-02 15:04"), line.FromAgent, line.ToAgent)
			if line.Status != "delivered" {
				fmt.Fprintf(out, " · %s", line.Status)
				if line.Detail != nil && *line.Detail != "" {
					fmt.Fprintf(out, " (%s)", *line.Detail)
				}
			}
			fmt.Fprintf(out, "\n  %s\n\n", line.Message)
		}
		return nil
	},
}

// resolveWorkspace turns what the user typed — a workspace name or its id —
// into the id the API's workspace paths take. Listing first (one GET) is what
// lets every workspace command accept the name people actually use.
func resolveWorkspace(ctx context.Context, client *api.Client, ref string) (string, error) {
	spaces, err := client.ListWorkspaces(ctx)
	if err != nil {
		return "", err
	}
	wanted := strings.ToLower(strings.TrimSpace(ref))
	for _, ws := range spaces {
		if ws.ID == ref || strings.ToLower(ws.Name) == wanted {
			return ws.ID, nil
		}
	}
	return "", fmt.Errorf("workspace not found: %s (see `chariot workspace list`)", ref)
}

// agentLabel is how a member is addressed in output: its name when it has one,
// else its slug.
func agentLabel(slug string, name *string) string {
	if name != nil && *name != "" {
		return *name
	}
	return slug
}

func printWorkspace(cmd *cobra.Command, detail *api.WorkspaceDetail) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "workspace %s (%s)\n", detail.Name, detail.ID)
	if len(detail.Agents) == 0 {
		fmt.Fprintf(out, "  no members — add some with `chariot workspace add %s <agent>...`\n", detail.Name)
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  SLUG\tNAME\tSTATE\tACTIVITY\tAGENT ID")
	for _, a := range detail.Agents {
		name := "-"
		if a.Name != nil {
			name = *a.Name
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", a.Slug, name, a.State, a.Activity, a.ID)
	}
	_ = tw.Flush()
}

func init() {
	workspaceDeleteCmd.Flags().BoolVarP(&workspaceDeleteYes, "yes", "y", false, "skip the confirmation prompt")
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceCreateCmd)
	workspaceCmd.AddCommand(workspaceShowCmd)
	workspaceCmd.AddCommand(workspaceDeleteCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceCrosstalkCmd)
	rootCmd.AddCommand(workspaceCmd)
}
