package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	listLimit int
	listAll   bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List your agents and their ids",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "AGENT ID\tSLUG\tSTATE")

		cursor := ""
		shown := 0
		for {
			page, err := client.ListAgents(cmd.Context(), cursor, listLimit)
			if err != nil {
				return err
			}
			for _, a := range page.Agents {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", a.ID, a.Slug, a.State)
				shown++
			}
			cursor = page.NextCursor
			if cursor == "" || !listAll {
				break
			}
		}
		tw.Flush()
		fmt.Fprintf(cmd.ErrOrStderr(), "\n%d agent(s) shown.\n", shown)
		if cursor != "" && !listAll {
			fmt.Fprintln(cmd.ErrOrStderr(), "More available — use --all to list every agent.")
		}
		return nil
	},
}

func init() {
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "page size")
	listCmd.Flags().BoolVar(&listAll, "all", false, "list every agent (paginate to the end)")
	rootCmd.AddCommand(listCmd)
}
