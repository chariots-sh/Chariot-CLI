package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var (
	goalWorkspace    string
	goalSetReplace   bool
	goalSetYes       bool
	goalCancelYes    bool
	goalHistoryLimit int
)

var goalCmd = &cobra.Command{
	Use:   "goal",
	Short: "Give a workspace agent a standing goal it works toward on its own",
	Long: `Give one workspace member a standing goal: an objective it keeps working
toward across turns, on its own schedule, until it completes, blocks, or you
cancel it.

  chariot goal set scout "ship the Q3 report" --workspace research
  chariot goal status scout --workspace research
  chariot goal pause scout --workspace research
  chariot goal resume scout --workspace research
  chariot goal cancel scout --workspace research
  chariot goal history scout --workspace research

An agent holds at most one open goal; setting a second one requires --replace.
The agent is addressed by id, slug, or name, the workspace by name or id.`,
	// Every subcommand acts on one workspace member, so the workspace is
	// required before anything talks to the backend.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if goalWorkspace == "" {
			return fmt.Errorf("--workspace is required (see `chariot workspace list`)")
		}
		return nil
	},
}

var goalSetCmd = &cobra.Command{
	Use:   "set <agent> [objective]",
	Short: "Set the agent's standing goal",
	Long: `Set the agent's standing goal. The objective comes from the argument, or
from stdin when omitted:

  chariot goal set scout "ship the Q3 report" --workspace research
  cat objective.md | chariot goal set scout --workspace research

An agent holds at most one open goal. If one exists, --replace supersedes it —
the standing work on the old goal stops.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		objective, err := goalObjective(args, cmd.InOrStdin())
		if err != nil {
			return err
		}
		wsID, err := resolveWorkspace(cmd.Context(), client, goalWorkspace)
		if err != nil {
			return err
		}
		goal, err := client.SetGoal(cmd.Context(), wsID, args[0], objective, false)
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "✓ goal set")
			printGoal(cmd, goal)
			return nil
		}
		// ONLY a 409 means "an open goal already exists" — anything else is
		// the real error.
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			return err
		}
		if !goalSetReplace {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", apiErr.Detail)
			fmt.Fprintf(cmd.ErrOrStderr(), "Replace it with --replace, or end it with `chariot goal cancel %s --workspace %s`.\n", args[0], goalWorkspace)
			return err
		}
		// Replacing supersedes standing work, so it prompts like other
		// destructive commands do.
		if !goalSetYes && !confirmGoal(cmd, fmt.Sprintf("This replaces %s's open goal — the standing work on it stops.", args[0])) {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		goal, err = client.SetGoal(cmd.Context(), wsID, args[0], objective, true)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ goal replaced")
		printGoal(cmd, goal)
		return nil
	},
}

var goalStatusCmd = &cobra.Command{
	Use:   "status <agent>",
	Short: "Show the agent's current goal and its recent events",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		wsID, err := resolveWorkspace(cmd.Context(), client, goalWorkspace)
		if err != nil {
			return err
		}
		goal, err := client.GetGoal(cmd.Context(), wsID, args[0])
		if err != nil {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				fmt.Fprintf(cmd.ErrOrStderr(), "No goal for %s yet — set one with `chariot goal set %s \"objective\" --workspace %s`.\n", args[0], args[0], goalWorkspace)
				return nil
			}
			return err
		}
		printGoal(cmd, goal)
		return nil
	},
}

var goalPauseCmd = &cobra.Command{
	Use:   "pause <agent>",
	Short: "Pause the goal's autonomous turns",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return goalTransition(cmd, args[0], "paused", (*api.Client).PauseGoal)
	},
}

var goalResumeCmd = &cobra.Command{
	Use:   "resume <agent>",
	Short: "Resume a paused goal",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return goalTransition(cmd, args[0], "resumed", (*api.Client).ResumeGoal)
	},
}

var goalCancelCmd = &cobra.Command{
	Use:   "cancel <agent>",
	Short: "Cancel the goal for good",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !goalCancelYes && !confirmGoal(cmd, fmt.Sprintf("This cancels %s's open goal — the standing work on it stops.", args[0])) {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		return goalTransition(cmd, args[0], "canceled", (*api.Client).CancelGoal)
	},
}

var goalHistoryCmd = &cobra.Command{
	Use:   "history <agent>",
	Short: "List the agent's goals, newest first",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if goalHistoryLimit < 1 {
			return fmt.Errorf("--limit must be at least 1")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		wsID, err := resolveWorkspace(cmd.Context(), client, goalWorkspace)
		if err != nil {
			return err
		}
		goals, err := client.GoalHistory(cmd.Context(), wsID, args[0], goalHistoryLimit)
		if err != nil {
			return err
		}
		if len(goals) == 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "No goals yet — set one with `chariot goal set %s \"objective\" --workspace %s`.\n", args[0], goalWorkspace)
			return nil
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CREATED\tSTATUS\tTURNS\tOBJECTIVE")
		for _, g := range goals {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", g.CreatedAt.Local().Format("2006-01-02 15:04"), g.Status, g.TurnsStarted, truncateText(g.Objective, 60))
		}
		return tw.Flush()
	},
}

// goalTransition runs one lifecycle action, reading the goal first so the
// transition carries the version just seen — a concurrent edit then 409s
// loudly (surfaced with the backend's detail) instead of silently winning.
func goalTransition(cmd *cobra.Command, agentRef, pastTense string, apply func(*api.Client, context.Context, string, string, *int) (*api.Goal, error)) error {
	client, _, err := authedClient()
	if err != nil {
		return err
	}
	wsID, err := resolveWorkspace(cmd.Context(), client, goalWorkspace)
	if err != nil {
		return err
	}
	current, err := client.GetGoal(cmd.Context(), wsID, agentRef)
	if err != nil {
		return err
	}
	goal, err := apply(client, cmd.Context(), wsID, agentRef, &current.Version)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ %s\n", pastTense)
	printGoal(cmd, goal)
	return nil
}

// goalObjective takes the objective from the second argument, else from stdin.
func goalObjective(args []string, stdin io.Reader) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	objective := strings.TrimSpace(string(data))
	if objective == "" {
		return "", fmt.Errorf("no objective — pass it as an argument or pipe it in on stdin")
	}
	return objective, nil
}

// confirmGoal asks a [y/N] question on the command's own streams.
func confirmGoal(cmd *cobra.Command, question string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s Continue? [y/N] ", question)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// truncateText caps s at max runes, marking the cut with an ellipsis.
func truncateText(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func printGoal(cmd *cobra.Command, g *api.Goal) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "objective : %s\n", g.Objective)
	fmt.Fprintf(out, "status    : %s\n", g.Status)
	fmt.Fprintf(out, "agent     : %s\n", agentLabel(g.AgentSlug, g.AgentName))
	fmt.Fprintf(out, "plan      : %s\n", orDash(g.Plan))
	fmt.Fprintf(out, "steering  : %s\n", orDash(g.LatestSteering))
	if g.Status == "blocked" {
		fmt.Fprintf(out, "blocked   : %s\n", orDash(g.BlockedReason))
	}
	if g.Status == "complete" {
		fmt.Fprintf(out, "summary   : %s\n", orDash(g.CompletedSummary))
		for _, e := range g.Evidence {
			fmt.Fprintf(out, "evidence  : %s\n", e)
		}
	}
	fmt.Fprintf(out, "version   : %d\n", g.Version)
	fmt.Fprintf(out, "turns     : %d\n", g.TurnsStarted)
	nextWake := "-"
	if g.NextWakeAt != nil {
		nextWake = g.NextWakeAt.Local().Format("2006-01-02 15:04")
	}
	fmt.Fprintf(out, "next wake : %s\n", nextWake)
	fmt.Fprintf(out, "updated   : %s\n", g.UpdatedAt.Local().Format("2006-01-02 15:04"))
	if len(g.RecentEvents) == 0 {
		return
	}
	fmt.Fprintf(out, "\nRecent events:\n")
	for _, ev := range g.RecentEvents {
		fmt.Fprintf(out, "  %s  %s/%s  %s\n", ev.CreatedAt.Local().Format("01-02 15:04"), ev.Actor, ev.Kind, truncateText(ev.Message, 100))
	}
}

func init() {
	goalCmd.PersistentFlags().StringVar(&goalWorkspace, "workspace", "", "workspace name or id (see `chariot workspace list`)")
	goalSetCmd.Flags().BoolVar(&goalSetReplace, "replace", false, "supersede the agent's open goal if it has one")
	goalSetCmd.Flags().BoolVarP(&goalSetYes, "yes", "y", false, "skip the confirmation prompt when replacing")
	goalCancelCmd.Flags().BoolVarP(&goalCancelYes, "yes", "y", false, "skip the confirmation prompt")
	goalHistoryCmd.Flags().IntVar(&goalHistoryLimit, "limit", 20, "how many goals to list")
	goalCmd.AddCommand(goalSetCmd)
	goalCmd.AddCommand(goalStatusCmd)
	goalCmd.AddCommand(goalPauseCmd)
	goalCmd.AddCommand(goalResumeCmd)
	goalCmd.AddCommand(goalCancelCmd)
	goalCmd.AddCommand(goalHistoryCmd)
	rootCmd.AddCommand(goalCmd)
}
