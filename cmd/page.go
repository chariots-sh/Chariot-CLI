package cmd

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
)

var pageAgent string

var pageCmd = &cobra.Command{
	Use:   "page --agent <agent-id>",
	Short: "Show the page an agent has published, and its shareable link",
	Long: `Show the HTML page one of your agents has published, and the link to it.

Any agent can publish a page — a report, a table, a status board — with no
skill to grant and no setup: it writes HTML to a file inside its own pod and
its instructions tell it how. Just ask one for a page, then run this.

The link needs no login, so anyone you send it to can open it. It is
unguessable and cannot be revoked, so treat the URL itself as the secret.

An agent has at most one page, and its link never changes: republishing
replaces the content in place, so a page an agent refreshes keeps the link you
already shared.

Nothing to show means one of two things, and they are indistinguishable: the
agent has not published a page, or it is asleep — the page lives on the agent's
own disk, so an idle agent's page is unreadable until it next wakes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pageAgent == "" {
			return fmt.Errorf("--agent is required (find ids with `chariot list`)")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		page, err := client.GetAgentPage(cmd.Context(), pageAgent)
		out := cmd.OutOrStdout()
		if err != nil {
			// Absence is a valid answer to a query, not a failure: report it
			// plainly and exit 0 so `chariot page` is safe in a loop.
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				fmt.Fprintf(out, "no page — %s hasn't published one, or it's asleep\n", pageAgent)
				return nil
			}
			return err
		}
		fmt.Fprintf(out, "title : %s\n", page.Title)
		fmt.Fprintf(out, "agent : %s\n", page.Agent)
		fmt.Fprintf(out, "size  : %d bytes of HTML\n", len(page.HTML))
		fmt.Fprintf(out, "link  : %s\n", page.ShareURL)
		fmt.Fprintf(out, "\nAnyone with that link can open the page without signing in.\n")
		return nil
	},
}

func init() {
	pageCmd.Flags().StringVar(&pageAgent, "agent", "", "agent id, slug, or name (see `chariot list`)")
	rootCmd.AddCommand(pageCmd)
}
