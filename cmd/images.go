package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List the agent images you can deploy (built-in + your custom images)",
	Long: `List the agent images you can deploy.

Chariot ships several agent images out of the box (ZeroClaw, OpenClaw,
NemoClaw, Hermes), and your own verified custom images
(` + "`chariot image push --name <name>`" + `) appear alongside them. Pick any of them per deploy with
` + "`chariot deploy --image <name>`" + ` — the choice is per agent, so different
agents can run different images. The daily fee follows each image's pod size.

Agents deployed without --image run your account default — shown as
"(default)" below. Change it with ` + "`chariot images set-default <name>`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		catalog, err := client.BuiltinImages(cmd.Context())
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "IMAGE\tPOD SIZE\tDAILY FEE\tSTATUS\tDESCRIPTION")
		for _, img := range catalog.Images {
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
		for _, img := range catalog.CustomImages {
			name := img.Name
			if img.Default {
				name += " (default)"
			}
			fmt.Fprintf(tw, "%s\t%s\t$%.2f\t%s\t%s\n",
				name, img.PodSize, img.DailyFeeDollars, "available", "Your custom image.")
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nDeploy onto one with `chariot deploy --count N --image <name>`.")
		fmt.Fprintln(cmd.OutOrStdout(), "Change the default with `chariot images set-default <name>`.")
		fmt.Fprintln(cmd.OutOrStdout(), "Add your own with `chariot image push --name <name>`.")
		return nil
	},
}

var imagesSetDefaultCmd = &cobra.Command{
	Use:   "set-default <name|default>",
	Short: "Choose the image agents deployed without --image run",
	Long: `Choose the image agents deployed without --image run — a built-in name or
one of your verified custom image names (` + "`chariot images`" + ` lists both).
Pass ` + "`default`" + ` to reset: your custom image named 'default' if one is
verified, else stock ZeroClaw.

Like an image push, the change applies to new activations immediately and to
running agents the next time they wake from hibernation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		choice := args[0]
		if choice == "default" { // reset to the implicit default
			choice = ""
		}
		effective, err := client.SetDefaultImage(cmd.Context(), choice)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ default image: %s\n", effective)
		fmt.Fprintln(out, "  New activations use it immediately; running agents adopt it on their next wake.")
		return nil
	},
}

func init() {
	imagesCmd.AddCommand(imagesSetDefaultCmd)
	rootCmd.AddCommand(imagesCmd)
}
