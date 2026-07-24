package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var apiCmd = &cobra.Command{
	Use:     "api",
	Aliases: []string{"docs"},
	Short:   "Print the HTTP API reference your service integrates against",
	Long: `Print the HTTP API your service calls once agents are deployed.

The CLI's job ends at deploying and managing the fleet. Sending messages to
agents and receiving their replies in production is done by your own service,
calling this API directly — not by wrapping the CLI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		base := cfg.BaseURL()
		fmt.Fprintf(cmd.OutOrStdout(), apiReference, base, base, base, base)
		return nil
	},
}

const apiReference = `CHARIOT HTTP API — what your service calls once agents are deployed.
Full docs: https://app.chariots.sh/docs

The ` + "`chariot demo`" + ` commands are one-off terminal stand-ins for the first two
calls below. Production integrations call these endpoints directly; do not
build on the CLI as a subprocess.

AUTH
  Messaging endpoints use the token-seed printed once by ` + "`chariot deploy`" + `,
  sent as the X-Chariot-Token header. Management endpoints use the session
  token from ` + "`chariot login`" + ` as Authorization: Bearer <token>.

SEND A MESSAGE TO AN AGENT
  POST %s/v1/agents/{agent-id}/messages
  header  X-Chariot-Token: <token-seed>
  body    {"message": "..."}
  → 202 {"status": "...", "agent_id": "...", "state": "..."}
  The agent replies asynchronously — via webhook and/or the reply inbox below.
  Agent ids come from ` + "`chariot list`" + ` or GET /v1/agents.

RECEIVE REPLIES — WEBHOOK (deploy with --endpoint)
  Chariot POSTs each reply to your endpoint:
  header  X-Chariot-Account: <account>
  body    {"agent_id": "...", "message": "...", "reply_to": "..."}
  Respond with any 2xx.

RECEIVE REPLIES — POLL THE INBOX (works with or without a webhook)
  GET %s/v1/replies?after=<cursor>&limit=<n>
  header  X-Chariot-Token: <token-seed>
  → {"replies": [{"id", "agent_id", "message", "reply_to", "created_at"}],
     "next_cursor": <id>}
  Start at after=0; pass next_cursor back as after on the next call.

LIST AGENTS
  GET %s/v1/agents?limit=<n>&cursor=<cursor>
  header  Authorization: Bearer <session-token>
  → {"agents": [{"id", "slug", "state"}], "next_cursor": "..."}

READ AN AGENT'S PUBLISHED PAGE
  GET %s/v1/agents/{agent-id}/page
  header  Authorization: Bearer <session-token>
  → {"title", "html", "agent", "share_url"}
  Any agent can publish a page — a report, a table, a status board — with no
  skill to grant and no API to call: it writes HTML to a file inside its own
  pod and hands you the link. Its instructions already tell it how, so just
  ask for one.
  The file IS the page, so this reads the pod every time. 404 does not mean
  "never published": a sleeping agent has no page until it wakes, and a
  deleted agent's page is gone for good. Don't poll this.
  share_url opens the same page with no login — the token is unguessable and
  cannot be revoked, so treat the URL as the secret.
  html is UNTRUSTED: an LLM wrote it. Render it in a sandboxed iframe without
  allow-same-origin. Never inject it into your own page, and never re-serve it
  as text/html from an origin that holds your session token.
`

func init() {
	rootCmd.AddCommand(apiCmd)
}
