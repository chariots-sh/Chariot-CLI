package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Show your account (credits, agents)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		a, err := client.Account(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Printf("email   : %s\n", a.Email)
		fmt.Printf("status  : %s\n", a.Status)
		fmt.Printf("credits : $%.2f\n", a.CreditDollars)
		fmt.Printf("agents  : %v\n", a.AgentsByState)
		fmt.Printf("tokens  : %v\n", a.TokenPrefixes)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountCmd)
}
