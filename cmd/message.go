package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var messageWait time.Duration

// How often the wait loop polls the inbox, how long it pauses before
// re-sending to an agent whose pod is still cold-starting (the backend's 503),
// and how long those re-sends go on for.
//
// The delivery window is deliberately independent of --wait: --wait is how
// long you'll wait for an ANSWER, while a 503 means the message was never
// delivered at all. Waking a hibernating agent is the common case, so even
// `--wait 0` waits the pod out — it just doesn't stay for the reply. Matches
// the backend's own cold-start budget for workspace delivery.
const (
	messagePollInterval  = 2 * time.Second
	messageDeliveryLimit = 3 * time.Minute
)

// messageStartingWait is a var so tests can drive the retry without sleeping.
var messageStartingWait = 10 * time.Second

var messageCmd = &cobra.Command{
	Use:     "message <agent> <message>",
	Aliases: []string{"msg"},
	Short:   "Message one of your agents and wait for its reply",
	Long: `Send a message to one of your agents and print the reply.

Authenticates with your ` + "`chariot login`" + ` session — no token-seed. Address the
agent by id, slug, or name (see ` + "`chariot list`" + `):

  chariot message research-bot "summarize today's filings"
  chariot message agent-000004 "status?" --wait 30s
  chariot message research-bot "long job, don't wait" --wait 0

A hibernating agent is woken by the message, and the send is retried while its
pod starts (up to 3 minutes) — that happens whatever --wait is set to, because
a message that never reached the agent has no reply coming. --wait governs only
how long to stay for the answer; --wait 0 returns as soon as the agent has the
message.

The reply lands in the inbox either way — read it later with ` + "`chariot inbox`" + `,
or receive it on the webhook your fleet was deployed with.

Building an application? Have your service call the API directly rather than
wrapping this command — run ` + "`chariot api`" + ` for the reference.`,
	Args: cobra.ExactArgs(2),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if messageWait < 0 {
			return fmt.Errorf("--wait cannot be negative (use --wait 0 to send without waiting for the reply)")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		agentRef, message := args[0], args[1]
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		// A correlation id picks THIS exchange's reply out of an inbox that
		// also carries every other agent's traffic. The agent echoes it back.
		correlation := "cli-" + randomToken()
		out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

		deliveryDeadline := time.Now().Add(messageDeliveryLimit)
		if messageWait <= 0 {
			ack, err := sendWaitingOutColdStart(ctx, client, agentRef, message, correlation, deliveryDeadline, errOut)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ %s — agent %s (%s)\n", ack.Status, ack.AgentID, ack.State)
			fmt.Fprintf(errOut, "  The reply arrives asynchronously — `chariot inbox --follow`.\n")
			return nil
		}

		// Start from the inbox's current end so the wait can't match a reply
		// that predates this send.
		cursor, err := inboxCursor(ctx, client)
		if err != nil {
			return err
		}
		ack, err := sendWaitingOutColdStart(ctx, client, agentRef, message, correlation, deliveryDeadline, errOut)
		if err != nil {
			return err
		}
		// The reply clock starts once the agent has the message.
		deadline := time.Now().Add(messageWait)
		fmt.Fprintf(errOut, "→ agent %s (%s) — waiting up to %s for a reply\n\n", ack.AgentID, ack.State, messageWait)

		reply, err := awaitReply(ctx, client, cursor, correlation, deadline)
		if err != nil {
			return err
		}
		if reply == nil {
			fmt.Fprintf(errOut, "No reply within %s. It is still coming — watch for it with `chariot inbox --follow`.\n", messageWait)
			return nil
		}
		fmt.Fprintln(out, reply.Message)
		return nil
	},
}

// randomToken is 12 hex chars of entropy — enough to keep two concurrent
// sends from colliding on one correlation id.
func randomToken() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is fatal-grade; a clock-based id still
		// distinguishes this send from the inbox's history.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// inboxCursor walks to the end of the reply inbox and returns that cursor,
// without printing the backlog.
func inboxCursor(ctx context.Context, client *api.Client) (int64, error) {
	var cursor int64
	for {
		page, err := client.Replies(ctx, cursor, 200)
		if err != nil {
			return 0, err
		}
		// An empty page — or a cursor that didn't move — is the end.
		if len(page.Replies) == 0 || page.NextCursor <= cursor {
			return page.NextCursor, nil
		}
		cursor = page.NextCursor
	}
}

// sendWaitingOutColdStart sends, re-sending while the backend reports the pod
// is still starting (503) — a hibernating agent's first message otherwise
// fails on a wake the caller asked for.
func sendWaitingOutColdStart(ctx context.Context, client *api.Client, agentRef, message, correlation string, deadline time.Time, errOut io.Writer) (*api.MessageAck, error) {
	notified := false
	for {
		ack, err := client.MessageAgent(ctx, agentRef, message, correlation)
		if err == nil {
			return ack, nil
		}
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || !time.Now().Before(deadline) {
			return nil, err
		}
		if !notified {
			fmt.Fprintf(errOut, "agent is waking up — retrying\n")
			notified = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(messageStartingWait):
		}
	}
}

// awaitReply polls the inbox for the reply carrying correlation, returning nil
// when the deadline passes first.
func awaitReply(ctx context.Context, client *api.Client, cursor int64, correlation string, deadline time.Time) (*api.Reply, error) {
	ticker := time.NewTicker(messagePollInterval)
	defer ticker.Stop()
	for {
		for { // drain every page available at this tick
			page, err := client.Replies(ctx, cursor, 200)
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil // interrupted mid-request
				}
				return nil, err
			}
			for i, reply := range page.Replies {
				if reply.ReplyTo != nil && *reply.ReplyTo == correlation {
					return &page.Replies[i], nil
				}
			}
			if len(page.Replies) == 0 || page.NextCursor <= cursor {
				break // drained: nothing new at this tick
			}
			cursor = page.NextCursor
		}
		if !time.Now().Before(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
		}
	}
}

func init() {
	messageCmd.Flags().DurationVar(&messageWait, "wait", 3*time.Minute, "how long to wait for the reply (0 sends without waiting)")
	rootCmd.AddCommand(messageCmd)
}
