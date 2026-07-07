package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List the built-in agent images you can deploy",
	Long: `List the built-in agent images you can deploy.

Chariot ships several agent images out of the box (ZeroClaw, OpenClaw,
NemoClaw, Hermes). Pick one per deploy with ` + "`chariot deploy --image <name>`" + ` —
the choice is per agent, so different agents can run different images. The
daily fee follows each image's pod size.

Agents deployed without --image run your account default: your verified
custom image (` + "`chariot image push`" + `) if you have one, else the stock image.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		images, err := client.BuiltinImages(cmd.Context())
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "IMAGE\tPOD SIZE\tDAILY FEE\tSTATUS\tDESCRIPTION")
		for _, img := range images {
			status := "available"
			if !img.Available {
				status = "coming soon"
			}
			name := img.Name
			if img.Default {
				name += " (default)"
			}
			fmt.Fprintf(tw, "%s\t%s\t$%.2f\t%s\t%s\n",
				name, img.PodSize, img.DailyFeeDollars, status, img.Description)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nDeploy onto one with `chariot deploy --count N --image <name>`.")
		fmt.Fprintln(cmd.OutOrStdout(), "Prefer your own image? `chariot image push` uploads a custom one.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(imagesCmd)
}
