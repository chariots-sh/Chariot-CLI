package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var (
	workspaceChatAgent  string
	workspaceChatWait   time.Duration
	workspaceChatFollow bool
	workspaceChatLimit  int
)

const workspaceChatPollInterval = 3 * time.Second

var workspaceChatCmd = &cobra.Command{
	Use:   "chat <workspace> [message]",
	Short: "Talk to a workspace — one member, or all of them at once",
	Long: `Send a message into a workspace thread and print the replies.

Without --agent the message is a broadcast: every member gets it, and each
reply is collected back into the same thread. With --agent it is a 1:1 with
that member. Omit the message to read the thread instead.

  chariot workspace chat research "who has capacity?"        # ask everyone
  chariot workspace chat research "start on part 2" --agent agent-000001
  chariot workspace chat research                            # read the thread
  chariot workspace chat research --agent agent-000001 --follow

Replies are asynchronous and members may be cold — the backend keeps trying
for about three minutes, so a reply can land after this command stops waiting.
Read it later with the same command, or --follow to keep watching.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		id, err := resolveWorkspace(ctx, client, args[0])
		if err != nil {
			return err
		}
		out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
		labels, err := memberLabels(ctx, client, id)
		if err != nil {
			return err
		}

		// Reading: print the thread's tail, then optionally keep watching.
		if len(args) == 1 {
			lines, cursor, err := readThread(ctx, client, id, workspaceChatAgent)
			if err != nil {
				return err
			}
			if len(lines) > workspaceChatLimit {
				lines = lines[len(lines)-workspaceChatLimit:]
			}
			for _, line := range lines {
				printChatLine(out, line, labels)
			}
			if len(lines) == 0 {
				fmt.Fprintln(errOut, "Nothing in this thread yet — send a message to start it.")
			}
			if !workspaceChatFollow {
				return nil
			}
			fmt.Fprintf(errOut, "watching the thread (every %s) — Ctrl-C to stop\n\n", workspaceChatPollInterval)
			return followThread(ctx, client, id, workspaceChatAgent, cursor, labels, out, 0, time.Time{})
		}

		// Sending: start the cursor at the thread's end, send, then print what
		// comes back until the replies arrive or the wait runs out.
		_, cursor, err := readThread(ctx, client, id, workspaceChatAgent)
		if err != nil {
			return err
		}
		sent, err := client.SendWorkspaceChat(ctx, id, workspaceChatAgent, args[1])
		if err != nil {
			return err
		}
		// The send's own line comes back on the next poll; don't reprint it.
		cursor = sent.ID

		expected := len(labels) // a broadcast is answered by every member
		if workspaceChatAgent != "" {
			expected = 1
		}
		if workspaceChatWait <= 0 {
			fmt.Fprintln(errOut, "✓ sent — read the replies with `chariot workspace chat "+args[0]+"`")
			return nil
		}
		fmt.Fprintf(errOut, "✓ sent — waiting up to %s for %d repl%s\n\n", workspaceChatWait, expected, plural(expected, "y", "ies"))
		deadline := time.Now().Add(workspaceChatWait)
		if err := followThread(ctx, client, id, workspaceChatAgent, cursor, labels, out, expected, deadline); err != nil {
			return err
		}
		return nil
	},
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// memberLabels maps each member's agent id to how it is shown in the thread
// (its name, else its slug).
func memberLabels(ctx context.Context, client *api.Client, id string) (map[string]string, error) {
	detail, err := client.GetWorkspace(ctx, id)
	if err != nil {
		return nil, err
	}
	labels := make(map[string]string, len(detail.Agents))
	for _, a := range detail.Agents {
		labels[a.ID] = agentLabel(a.Slug, a.Name)
	}
	return labels, nil
}

// readThread pages one thread to its end, returning every line and the cursor
// to poll from next.
func readThread(ctx context.Context, client *api.Client, id, agentRef string) ([]api.ChatLine, int64, error) {
	var (
		lines  []api.ChatLine
		cursor int64
	)
	for {
		page, err := client.WorkspaceChat(ctx, id, agentRef, cursor, 200)
		if err != nil {
			return nil, 0, err
		}
		lines = append(lines, page.Messages...)
		// An empty page — or a cursor that didn't move — is the end.
		if len(page.Messages) == 0 || page.NextCursor <= cursor {
			return lines, page.NextCursor, nil
		}
		cursor = page.NextCursor
	}
}

// followThread prints new thread lines as they arrive. It stops on the
// deadline, or once expected agent replies have been printed (expected 0 and a
// zero deadline mean "follow until interrupted").
func followThread(ctx context.Context, client *api.Client, id, agentRef string, cursor int64, labels map[string]string, out io.Writer, expected int, deadline time.Time) error {
	ticker := time.NewTicker(workspaceChatPollInterval)
	defer ticker.Stop()
	replies := 0
	for {
		for { // drain every page available at this tick
			page, err := client.WorkspaceChat(ctx, id, agentRef, cursor, 200)
			if err != nil {
				if ctx.Err() != nil {
					return nil // interrupted mid-request
				}
				return err
			}
			for _, line := range page.Messages {
				printChatLine(out, line, labels)
				if line.Sender == "agent" {
					replies++
				}
			}
			if len(page.Messages) == 0 || page.NextCursor <= cursor {
				cursor = page.NextCursor
				break // drained: nothing new at this tick
			}
			cursor = page.NextCursor
		}
		if expected > 0 && replies >= expected {
			return nil
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func printChatLine(out io.Writer, line api.ChatLine, labels map[string]string) {
	who := "you"
	if line.Sender == "agent" {
		who = "agent"
		if line.AgentID != nil {
			if label, ok := labels[*line.AgentID]; ok {
				who = label
			} else {
				who = *line.AgentID
			}
		}
	}
	fmt.Fprintf(out, "[%s] %s\n  %s\n\n", line.CreatedAt.Local().Format("01-02 15:04"), who, line.Message)
}

func init() {
	workspaceChatCmd.Flags().StringVar(&workspaceChatAgent, "agent", "", "message one member (id, slug, or name) instead of broadcasting")
	workspaceChatCmd.Flags().DurationVar(&workspaceChatWait, "wait", 3*time.Minute, "how long to wait for replies after sending (0 sends without waiting)")
	workspaceChatCmd.Flags().BoolVar(&workspaceChatFollow, "follow", false, "when reading, keep printing new lines")
	workspaceChatCmd.Flags().IntVar(&workspaceChatLimit, "limit", 20, "how many recent lines to print when reading")
	workspaceCmd.AddCommand(workspaceChatCmd)
}
