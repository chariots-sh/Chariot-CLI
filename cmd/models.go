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
https://openrouter.ai/models. The choice applies to your whole fleet and takes
effect immediately: every agent model call goes through the Chariot proxy,
which routes it to your current choice and bills exactly what OpenRouter
charges for each call.
Change it with ` + "`chariot models set <model-id>`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		a, err := client.Account(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Printf("fleet model : %s\n", a.Model)
		fmt.Println("\nAny OpenRouter model id works — https://openrouter.ai/models")
		fmt.Println("Change it with `chariot models set <model-id>` (or `set default`).")
		return nil
	},
}

var modelsSetCmd = &cobra.Command{
	Use:   "set <model-id|default>",
	Short: "Choose the model your fleet uses (takes effect immediately)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		choice := args[0]
		if choice == "default" { // reset to the server default
			choice = ""
		}
		effective, err := client.SetModel(cmd.Context(), choice)
		if err != nil {
			return err
		}
		fmt.Printf("✓ fleet model: %s\n", effective)
		return nil
	},
}

func init() {
	modelsCmd.AddCommand(modelsSetCmd)
	rootCmd.AddCommand(modelsCmd)
}
