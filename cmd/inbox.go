package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/demo"
	"github.com/spf13/cobra"
)

var (
	inboxFollow   bool
	inboxInterval time.Duration
	inboxLimit    int
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read the replies your agents have sent",
	Long: `Print your agents' replies from the server-side inbox.

Every reply is stored whether or not your fleet has a webhook, so this works
with no tunnel and no token-seed — your ` + "`chariot login`" + ` session is enough.

  chariot inbox              # the most recent replies
  chariot inbox --follow     # keep printing new ones (Ctrl-C to stop)

A production service should poll GET /v1/replies (or receive webhooks) itself
rather than parse this output — run ` + "`chariot api`" + ` for the reference.`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if inboxLimit < 1 {
			return fmt.Errorf("--limit must be at least 1")
		}
		if inboxInterval <= 0 {
			return fmt.Errorf("--interval must be positive (e.g. 2s)")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

		// One walk to the end collects the tail: the inbox is ordered by id
		// with no "latest first" query, so the last page IS the recent ones.
		var cursor int64
		recent := make([]demo.Reply, 0, inboxLimit)
		times := make([]time.Time, 0, inboxLimit)
		for {
			page, err := client.Replies(ctx, cursor, 200)
			if err != nil {
				return err
			}
			for _, r := range page.Replies {
				recent = append(recent, demo.Reply{AgentID: r.AgentID, Message: r.Message, ReplyTo: r.ReplyTo})
				times = append(times, r.CreatedAt.Local())
			}
			if len(recent) > inboxLimit {
				recent, times = recent[len(recent)-inboxLimit:], times[len(times)-inboxLimit:]
			}
			// An empty page — or a cursor that didn't move — is the end.
			if len(page.Replies) == 0 || page.NextCursor <= cursor {
				cursor = page.NextCursor
				break
			}
			cursor = page.NextCursor
		}
		for i, r := range recent {
			demo.PrintReply(out, times[i], r.AgentID, "", r.ReplyTo, r.Message)
		}
		if len(recent) == 0 {
			fmt.Fprintln(errOut, "No replies yet — send one with `chariot message <agent> \"...\"`.")
		}
		if !inboxFollow {
			return nil
		}

		fmt.Fprintf(errOut, "watching for replies (every %s) — Ctrl-C to stop\n\n", inboxInterval)
		ticker := time.NewTicker(inboxInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				fmt.Fprintln(errOut, "stopped")
				return nil
			case <-ticker.C:
			}
			for { // drain all pages at this tick
				page, err := client.Replies(ctx, cursor, 200)
				if err != nil {
					if ctx.Err() != nil {
						return nil // interrupted mid-request
					}
					return err
				}
				for _, r := range page.Replies {
					demo.PrintReply(out, r.CreatedAt.Local(), r.AgentID, "", r.ReplyTo, r.Message)
				}
				if len(page.Replies) == 0 || page.NextCursor <= cursor {
					cursor = page.NextCursor
					break
				}
				cursor = page.NextCursor
			}
		}
	},
}

func init() {
	inboxCmd.Flags().BoolVar(&inboxFollow, "follow", false, "keep printing replies as they arrive")
	inboxCmd.Flags().DurationVar(&inboxInterval, "interval", 2*time.Second, "poll interval with --follow")
	inboxCmd.Flags().IntVar(&inboxLimit, "limit", 20, "how many recent replies to print first")
	rootCmd.AddCommand(inboxCmd)
}
