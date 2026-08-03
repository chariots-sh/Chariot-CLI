package cmd

import (
	"bufio"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var workspaceGuestsRemoveYes bool

var workspaceGuestsCmd = &cobra.Command{
	Use:   "guests <workspace>",
	Short: "Show who may chat with this workspace's agents",
	Long: `Show the workspace's invited guests: people (non-builders) who sign in at
the web app's /g page with just their email and chat with the member agents
you granted them. A guest shares the SAME 1:1 thread you see in the web
chat, and messages they send bill your account.

Invite (or extend) with ` + "`chariot workspace guests add`" + `; revoke with ` + "`remove`" + `.

Examples:
  chariot workspace guests research-team
  chariot workspace guests add research-team alice@example.com scout writer
  chariot workspace guests remove research-team alice@example.com scout
  chariot workspace guests remove research-team alice@example.com --yes`,
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
		guests, err := client.ListWorkspaceGuests(cmd.Context(), id)
		if err != nil {
			return err
		}
		printWorkspaceGuests(cmd, args[0], guests)
		return nil
	},
}

var workspaceGuestsAddCmd = &cobra.Command{
	Use:   "add <workspace> <email> <agent>...",
	Short: "Invite a guest to chat with member agents",
	Long: `Grant a guest chat access to one or more member agents (id, slug, or name).
Their first grant in the workspace emails them an invite with the sign-in
link; extending an existing guest's access is silent. Re-granting an agent
they already have is a no-op.`,
	Args: cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		guests, err := client.AddWorkspaceGuest(cmd.Context(), id, args[1], args[2:])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ granted %s chat access to %s in %s\n\n",
			args[1], strings.Join(args[2:], ", "), args[0])
		printWorkspaceGuests(cmd, args[0], guests)
		return nil
	},
}

var workspaceGuestsRemoveCmd = &cobra.Command{
	Use:   "remove <workspace> <email> [agent]",
	Short: "Revoke a guest's chat access",
	Long: `Revoke a guest's access — to one member agent, or to every agent in the
workspace when no agent is named. The shared chat history stays; the guest
just can't open the thread anymore. Idempotent.`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		id, err := resolveWorkspace(cmd.Context(), client, args[0])
		if err != nil {
			return err
		}
		agentRef := ""
		if len(args) == 3 {
			agentRef = args[2]
		}
		if agentRef == "" && !workspaceGuestsRemoveYes {
			fmt.Fprintf(cmd.OutOrStdout(), "This revokes ALL of %s's chat access in %s. Continue? [y/N] ", args[1], args[0])
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		guests, err := client.RemoveWorkspaceGuest(cmd.Context(), id, args[1], agentRef)
		if err != nil {
			return err
		}
		if agentRef == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "✓ revoked all of %s's access in %s\n\n", args[1], args[0])
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "✓ revoked %s's access to %s\n\n", args[1], agentRef)
		}
		printWorkspaceGuests(cmd, args[0], guests)
		return nil
	},
}

func printWorkspaceGuests(cmd *cobra.Command, workspace string, guests []api.WorkspaceGuest) {
	if len(guests) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "No guests in %s. Invite one with `chariot workspace guests add %s <email> <agent>...`\n",
			workspace, workspace)
		return
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tAGENTS\tINVITED")
	for _, g := range guests {
		labels := make([]string, 0, len(g.Agents))
		for _, a := range g.Agents {
			labels = append(labels, agentLabel(a.Slug, a.Name))
		}
		agents := "-"
		if len(labels) > 0 {
			agents = strings.Join(labels, ", ")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", g.Email, agents, g.InvitedAt.Local().Format("2006-01-02"))
	}
	_ = tw.Flush()
}

func init() {
	workspaceGuestsRemoveCmd.Flags().BoolVarP(&workspaceGuestsRemoveYes, "yes", "y", false, "skip the confirmation prompt")
	workspaceGuestsCmd.AddCommand(workspaceGuestsAddCmd)
	workspaceGuestsCmd.AddCommand(workspaceGuestsRemoveCmd)
	workspaceCmd.AddCommand(workspaceGuestsCmd)
}
