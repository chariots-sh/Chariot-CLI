package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List the models your agents can use",
	Long: `List the models your agents can use, with prices per 1M tokens.

The selected model applies to your whole fleet and takes effect immediately —
every agent model call goes through the Chariot proxy, which routes it to your
current choice. Change it with ` + "`chariot models set <model-id>`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		models, err := client.ListModels(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Printf("  %-40s %10s %10s\n", "MODEL", "IN $/1M", "OUT $/1M")
		for _, m := range models {
			marker := " "
			if m.Selected {
				marker = "*"
			}
			suffix := ""
			if m.IsDefault {
				suffix = "  (default)"
			}
			fmt.Printf("%s %-40s %10.2f %10.2f%s\n",
				marker, m.ID, m.InputUSDPer1MTokens, m.OutputUSDPer1MTokens, suffix)
		}
		fmt.Println("\n* = your fleet's current model — change with `chariot models set <model-id>`")
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
