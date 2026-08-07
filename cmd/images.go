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
NemoClaw, Hermes); your own verified custom images
(` + "`chariot image push --name <name>`" + `) and images shared with you (bound
when you deployed another account's fleet pack) appear alongside them. Pick any
of them per deploy with ` + "`chariot deploy --image <name>`" + ` — the choice is per
agent, so different agents can run different images — and swap an existing
agent with ` + "`chariot images set <name> --agent <agent>`" + `. The daily fee
follows each image's pod size.

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
		for _, img := range catalog.SharedImages {
			name := img.Name
			if img.Default {
				name += " (default)"
			}
			fee := "-"
			if img.DailyFeeDollars != nil {
				fee = fmt.Sprintf("$%.2f", *img.DailyFeeDollars)
			}
			desc := fmt.Sprintf("Shared by %s.", img.OwnerEmail)
			if img.HasSkill {
				desc += " Setup guide available."
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				name, orDash(img.PodSize), fee, shareStatusText(img.Status), desc)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nDeploy onto one with `chariot deploy --count N --image <name>`.")
		fmt.Fprintln(cmd.OutOrStdout(), "Swap one agent onto another with `chariot images set <name> --agent <agent>`.")
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

var imagesSetAgent string

var imagesSetCmd = &cobra.Command{
	Use:   "set <name|default> --agent <agent>",
	Short: "Swap the image one agent runs (its per-agent override)",
	Long: `Swap the image ONE agent runs — a built-in name, one of your verified custom
image names, or an accepted share alias (` + "`chariot images`" + ` lists everything
deployable). ` + "`--agent`" + ` takes the agent's id, slug, or name (find them with
` + "`chariot list`" + `); ` + "`default`" + ` clears the override so the agent falls back to
your account default.

A running agent is re-imaged in place: its pod restarts on the new image and
its workspace is kept. A hibernating or never-activated agent picks the image
up when it next starts. Either way the daily active fee follows the new
image's pod size.

    chariot images set openclaw --agent agent-000003
    chariot images set default --agent scout      # back to the account default

Per-agent overrides show in the IMAGE column of ` + "`chariot list`" + `. For the
account-wide default, use ` + "`chariot images set-default <name>`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if imagesSetAgent == "" {
			return fmt.Errorf("images set swaps one agent — pass --agent <agent>; for the account-wide default use `chariot images set-default <name>`")
		}
		choice := args[0]
		if choice == "default" { // clear the override back to the account default
			choice = ""
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		agent, err := client.SetAgentImage(cmd.Context(), imagesSetAgent, choice)
		if err != nil {
			return err
		}
		image := "account default" // stamp cleared: the agent follows `images set-default`
		if agent.Image != nil {
			image = *agent.Image
		}
		out := cmd.OutOrStdout()
		if agent.Applied {
			fmt.Fprintf(out, "✓ %s → %s (%s pod) — pod re-imaged in place, workspace kept\n", agent.Slug, image, agent.PodSize)
		} else {
			fmt.Fprintf(out, "✓ %s → %s (%s pod) — agent is %s; applies when it next starts\n", agent.Slug, image, agent.PodSize, agent.State)
		}
		fmt.Fprintln(out, "  The daily active fee follows the pod size.")
		return nil
	},
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

// shareStatusText renders a share's lifecycle status for humans. Shares are
// bound by deploying a fleet pack; state is managed on the web account page.
func shareStatusText(status string) string {
	switch status {
	case "pending":
		return "pending"
	case "active":
		return "available"
	case "owner_repushing":
		return "unavailable (owner re-pushing)"
	case "tier_raised":
		return "needs re-consent (pod tier raised) — redeploy the pack or manage on the web"
	}
	return status
}

func init() {
	imagesSetCmd.Flags().StringVar(&imagesSetAgent, "agent", "", "the agent to swap — id, slug, or name (required)")
	imagesCmd.AddCommand(imagesSetCmd)
	imagesCmd.AddCommand(imagesSetDefaultCmd)
	rootCmd.AddCommand(imagesCmd)
}
