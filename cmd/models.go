package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Show your fleet's model (any OpenRouter model works)",
	Long: `Show the model your fleet runs on.

Your fleet can run on ANY model OpenRouter serves — browse them at
https://openrouter.ai/models. This is the fleet DEFAULT: every agent that
hasn't been given its own model runs it. The choice takes effect immediately —
every agent model call goes through the Chariot proxy, which routes it to the
agent's model (or this default) and bills exactly what OpenRouter charges.

Change the default with ` + "`chariot models set <model-id>`" + `. Override one agent
with ` + "`chariot models set <model-id> --agent <agent-id>`" + `, or set a model on a
fresh batch at ` + "`chariot deploy --model`" + `.

Your fleet can also run on a model YOU host on a dedicated GPU — open-weight
catalog models or your own weights — addressable as ` + "`self/<name>`" + ` through
this same ` + "`models set`" + `. See ` + "`chariot models catalog`" + ` and
` + "`chariot models push --help`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		a, err := client.Account(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "fleet default model : %s\n", a.Model)
		fmt.Fprintln(out, "\nAny OpenRouter model id works — https://openrouter.ai/models")
		fmt.Fprintln(out, "Change the default with `chariot models set <model-id>` (or `set default`).")
		fmt.Fprintln(out, "Override one agent with `chariot models set <model-id> --agent <agent-id>`.")
		fmt.Fprintln(out, "Per-agent overrides show in the MODEL column of `chariot list`.")
		return nil
	},
}

var modelsSetAgent string

var modelsSetCmd = &cobra.Command{
	Use:   "set <model-id|default>",
	Short: "Choose the model your fleet — or one agent (--agent) — uses",
	Long: `Choose the model your fleet runs on.

Without --agent, sets the fleet DEFAULT (any OpenRouter model,
https://openrouter.ai/models) — every agent that hasn't been given its own
model. Pass ` + "`default`" + ` to reset to the server default.

With ` + "`--agent <agent-id>`" + `, overrides just that one agent (find ids with
` + "`chariot list`" + `); ` + "`default`" + ` clears the override so the agent falls back to
the fleet default. Either way it takes effect immediately.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		choice := args[0]
		if choice == "default" { // reset to the (fleet or account) default
			choice = ""
		}
		if modelsSetAgent != "" {
			effective, err := client.SetAgentModel(cmd.Context(), modelsSetAgent, choice)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ agent %s model: %s\n", modelsSetAgent, effective)
			return nil
		}
		effective, err := client.SetModel(cmd.Context(), choice)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ fleet model: %s\n", effective)
		return nil
	},
}

func init() {
	modelsSetCmd.Flags().StringVar(&modelsSetAgent, "agent", "", "override just this agent — id, slug, or name (default: the whole fleet)")
	modelsCmd.AddCommand(modelsSetCmd)
	rootCmd.AddCommand(modelsCmd)
}
