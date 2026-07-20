package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	deployCount    int
	deployEndpoint string
	deployModel    string
	deployImage    string
	deploySkills   []string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Create a fleet of agents",
	Long: `Create a fleet of agents.

Agents start deactivated and cost nothing until you message them. Deploy prints
a token-seed (shown once) — your backend uses it, together with an agent id from
` + "`chariot list`" + `, to send messages.

With --endpoint, agents POST replies to that URL. Without it, replies are
stored in the reply inbox — your service polls GET /v1/replies (or try
` + "`chariot demo watch`" + ` once from a terminal). Run ` + "`chariot api`" + ` for the full
HTTP reference your service integrates against.

With --model, the created agents run that model — any model OpenRouter serves
(https://openrouter.ai/models). The choice is per deploy, so different agents
can run different models. Without it, agents run your fleet default
(` + "`chariot models set`" + `); retune one later with
` + "`chariot models set <model-id> --agent <agent-id>`" + `.

With --image, the created agents run that agent image — a built-in (zeroclaw,
openclaw, nemoclaw, hermes) or one of your verified custom images by name
(` + "`chariot image push --name`" + `); ` + "`chariot images`" + ` lists everything available.
The choice is per deploy, so different agents can run different images.
Without it, agents run your account default (` + "`chariot images set-default`" + `).

With --skills, the created agents are granted extra tools. Available skills:

  docs   shared documents between your agents — each agent can read every
         document, add its own, and append to the others'; you manage the
         document spaces and their membership in the web app.

Unknown skill names fail the deploy and list what's available.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if deployCount <= 0 {
			return fmt.Errorf("--count must be positive")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		res, err := client.Deploy(cmd.Context(), deployCount, deployEndpoint, deployModel, deployImage, deploySkills)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "\n✓ %d agents live (total %d)\n\n", res.Created, res.Total)
		if deployEndpoint != "" {
			fmt.Fprintf(out, "  endpoint    : %s\n", deployEndpoint)
		} else {
			fmt.Fprintln(out, "  endpoint    : none — replies go to the inbox (`chariot demo watch`)")
		}
		fmt.Fprintf(out, "  model       : %s\n", res.Model)
		if res.Image != "" {
			fmt.Fprintf(out, "  image       : %s\n", res.Image)
		}
		if len(res.Skills) > 0 {
			fmt.Fprintf(out, "  skills      : %s\n", strings.Join(res.Skills, ", "))
		}
		fmt.Fprintf(out, "  namespace   : %s\n", res.Namespace)
		fmt.Fprintf(out, "  token-seed  : %s\n", res.TokenSeed)
		fmt.Fprintln(out, "\n  Save the token-seed now — it is shown only once.")
		fmt.Fprintln(out, "  Next: your service messages agents over the HTTP API:")
		fmt.Fprintln(out, "    POST {api-base}/v1/agents/{agent-id}/messages")
		fmt.Fprintln(out, "    header  X-Chariot-Token: "+res.TokenSeed)
		fmt.Fprintln(out, "  Find agent ids with `chariot list`.")
		fmt.Fprintln(out, "  Full API reference (send, replies, webhooks): `chariot api` · https://app.chariots.sh/docs")
		return nil
	},
}

func init() {
	deployCmd.Flags().IntVar(&deployCount, "count", 0, "number of agents to create")
	deployCmd.Flags().StringVar(&deployEndpoint, "endpoint", "", "webhook URL your agents reply to (optional; omit for inbox-only)")
	deployCmd.Flags().StringVar(&deployModel, "model", "", "model these agents run (optional; any OpenRouter model id; per deploy)")
	deployCmd.Flags().StringVar(&deployImage, "image", "", "agent image for these agents — built-in or custom name (optional; see `chariot images`)")
	deployCmd.Flags().StringSliceVar(&deploySkills, "skills", nil, "skills granted to these agents, comma-separated (optional; e.g. docs)")
	rootCmd.AddCommand(deployCmd)
}
