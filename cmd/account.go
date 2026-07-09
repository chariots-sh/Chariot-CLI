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
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "email     : %s\n", a.Email)
		fmt.Fprintf(out, "status    : %s\n", a.Status)
		fmt.Fprintf(out, "credits   : $%.2f\n", a.CreditDollars)
		fmt.Fprintf(out, "model     : %s\n", a.Model)
		fmt.Fprintf(out, "image     : %s (default for agents deployed without --image)\n", a.DefaultImage)
		fmt.Fprintf(out, "hibernate : after %s idle (dd:hh:mm)\n", formatDDHHMM(a.HibernateAfterSeconds))
		fmt.Fprintf(out, "agents    : %v\n", a.AgentsByState)
		fmt.Fprintf(out, "tokens    : %v\n", a.TokenPrefixes)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountCmd)
}
