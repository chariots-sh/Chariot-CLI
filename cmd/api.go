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
		fmt.Fprintf(cmd.OutOrStdout(), apiReference, base, base, base, base, base, base)
		return nil
	},
}

const apiReference = `CHARIOT HTTP API — what your service calls once agents are deployed.
Full docs: https://app.chariots.sh/docs

The ` + "`chariot demo`" + ` commands are one-off terminal stand-ins for the first two
calls below. Production integrations call these endpoints directly; do not
build on the CLI as a subprocess.

AUTH
  Messaging endpoints take EITHER credential: the token-seed printed once by
  ` + "`chariot deploy`" + ` (X-Chariot-Token header), or the session token from
  ` + "`chariot login`" + ` (Authorization: Bearer <token>). Give a service the
  token-seed — it is scoped to messaging and carries no login. The session
  token is what lets the CLI message an agent for you (` + "`chariot message`" + `).
  Management endpoints take the session token only.

SEND A MESSAGE TO AN AGENT
  POST %s/v1/agents/{agent-id}/messages
  header  X-Chariot-Token: <token-seed>    (or Authorization: Bearer <session>)
  body    {"message": "...", "reply_to": "<your correlation id, optional>"}
  → 202 {"status": "...", "agent_id": "...", "state": "..."}
  The agent replies asynchronously — via webhook and/or the reply inbox below.
  reply_to comes back on the reply, which is how you match it to this send.
  Agent ids come from ` + "`chariot list`" + ` or GET /v1/agents.

RECEIVE REPLIES — WEBHOOK (deploy with --endpoint)
  Chariot POSTs each reply to your endpoint:
  header  X-Chariot-Account: <account>
  body    {"agent_id": "...", "message": "...", "reply_to": "..."}
  Respond with any 2xx.

RECEIVE REPLIES — POLL THE INBOX (works with or without a webhook)
  GET %s/v1/replies?after=<cursor>&limit=<n>
  header  X-Chariot-Token: <token-seed>    (or Authorization: Bearer <session>)
  → {"replies": [{"id", "agent_id", "message", "reply_to", "created_at"}],
     "next_cursor": <id>}
  Start at after=0; pass next_cursor back as after on the next call.
  ` + "`chariot inbox`" + ` reads this same inbox with your login.

LIST AGENTS
  GET %s/v1/agents?limit=<n>&cursor=<cursor>
  header  Authorization: Bearer <session-token>
  → {"agents": [{"id", "slug", "state"}], "next_cursor": "..."}

GROUP AGENTS INTO A WORKSPACE
  GET  %s/v1/workspaces                       # and POST to create
  POST /v1/workspaces/{id}/agents             # add members (id, slug, or name)
  POST /v1/workspaces/{id}/chat               # {"message", "agent_ref"?}
  GET  /v1/workspaces/{id}/chat?after=<cursor>&agent_ref=<ref>
  GET  /v1/workspaces/{id}/documents          # the members' shared documents
  header  Authorization: Bearer <session-token>
  A workspace is a set of agents with one chat thread per member plus a
  broadcast thread (omit agent_ref and every member gets the message, each
  reply collected back into the same thread), a shared document store the
  agents read and write, and agent-to-agent messaging between members. Sends
  are fire-and-forget: 202 stores your line, replies land in the thread you
  poll. The CLI drives all of it — ` + "`chariot workspace --help`" + `.

GIVE A WORKSPACE AGENT A STANDING GOAL
  POST %s/v1/workspaces/{id}/agents/{ref}/goal        # {"objective", "replace"}
  GET  /v1/workspaces/{id}/agents/{ref}/goal          # current goal (404 = never had one)
  POST /v1/workspaces/{id}/agents/{ref}/goal/pause    # {"expected_version"?}
  POST /v1/workspaces/{id}/agents/{ref}/goal/resume   # {"expected_version"?}
  POST /v1/workspaces/{id}/agents/{ref}/goal/cancel   # {"expected_version"?}
  GET  /v1/workspaces/{id}/agents/{ref}/goal/history?limit=<n>
  header  Authorization: Bearer <session-token>
  A goal is a standing objective one workspace member keeps working toward on
  its own schedule until it completes, blocks, or is canceled. {ref} is the
  agent's id, slug, or name. An agent holds at most one open goal: setting a
  second answers 409 unless "replace" is true. Transitions take the goal's
  version as expected_version so a concurrent edit 409s instead of being
  overwritten; cancel is idempotent. The CLI drives all of it — see
  ` + "`chariot goal --help`" + `.

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
