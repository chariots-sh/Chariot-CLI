package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/Immortal-Protocols/Chariot-CLI/internal/demo"
	"github.com/spf13/cobra"
)

var (
	demoWatchToken    string
	demoWatchInterval time.Duration
	demoWatchFromNow  bool
)

var demoWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Print agent replies in the terminal (demo only — not for scripting)",
	Long: `Poll the reply inbox and print agent replies to the terminal.

Every reply an agent sends is stored server-side; this polls GET /v1/replies
and prints new ones as they arrive — no public webhook or tunnel required.

Demo only — a production service should call GET /v1/replies itself (or
receive webhooks) rather than parse this command's output. Run ` + "`chariot api`" + `
for the reference.

Authenticates with the token-seed from ` + "`chariot deploy`" + ` (--token or
CHARIOT_TOKEN_SEED). Starts from the beginning of the inbox; --from-now skips
the backlog. Stop with Ctrl-C.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := demoWatchToken
		if token == "" {
			token = os.Getenv("CHARIOT_TOKEN_SEED")
		}
		if token == "" {
			return fmt.Errorf("token-seed required — pass --token or set CHARIOT_TOKEN_SEED (printed once by `chariot deploy`)")
		}
		// Token-seed auth, no login needed — config is only read for the base URL.
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		client := api.New(cfg.BaseURL(), "")

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		var cursor int64
		if demoWatchFromNow {
			cursor, err = drainCursor(ctx, client, token)
			if err != nil {
				return err
			}
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "watching for replies (every %s) — Ctrl-C to stop\n\n", demoWatchInterval)
		ticker := time.NewTicker(demoWatchInterval)
		defer ticker.Stop()
		for {
			for { // drain all pages at this tick
				page, err := client.ListReplies(ctx, token, cursor, 200)
				if err != nil {
					if ctx.Err() != nil {
						return nil // interrupted mid-request
					}
					return err
				}
				for _, r := range page.Replies {
					demo.PrintReply(cmd.OutOrStdout(), r.CreatedAt.Local(), r.AgentID, "", r.ReplyTo, r.Message)
				}
				cursor = page.NextCursor
				if len(page.Replies) == 0 {
					break
				}
			}
			select {
			case <-ctx.Done():
				fmt.Fprintln(cmd.ErrOrStderr(), "stopped")
				return nil
			case <-ticker.C:
			}
		}
	},
}

// drainCursor advances past the existing backlog without printing it.
func drainCursor(ctx context.Context, client *api.Client, token string) (int64, error) {
	var cursor int64
	for {
		page, err := client.ListReplies(ctx, token, cursor, 200)
		if err != nil {
			return 0, err
		}
		cursor = page.NextCursor
		if len(page.Replies) == 0 {
			return cursor, nil
		}
	}
}

func init() {
	demoWatchCmd.Flags().StringVar(&demoWatchToken, "token", "", "token-seed from `chariot deploy` (or CHARIOT_TOKEN_SEED)")
	demoWatchCmd.Flags().DurationVar(&demoWatchInterval, "interval", 2*time.Second, "poll interval")
	demoWatchCmd.Flags().BoolVar(&demoWatchFromNow, "from-now", false, "skip existing replies; only print new ones")
	demoCmd.AddCommand(demoWatchCmd)
}
