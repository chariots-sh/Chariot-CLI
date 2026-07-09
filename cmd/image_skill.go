package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
)

var imageSkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Read or manage an image's setup guide (for you or your coding agent)",
	Long: `Read or manage the setup guide attached to a custom image — a markdown
document written for the people (and their coding agents) who deploy the
image: what first message to send, what the webhook payloads look like, how
to wire the fleet into a backend. It is not delivered into agent pods.

The guide lives on the image name, travels with every share of it, and
survives re-pushes.

  chariot image skill show research            # print it (pipe to a file or agent)
  chariot image skill set research SKILL.md    # attach/replace yours
  chariot image skill clear research           # remove yours`,
}

var imageSkillFrom string

// findIncomingShare returns the incoming share the name refers to: an
// accepted share by its alias, or a pending offer by its image name.
func findIncomingShare(shares *api.Shares, name, from string) (*api.IncomingShare, error) {
	var matches []api.IncomingShare
	for _, s := range shares.Incoming {
		if from != "" && s.OwnerEmail != from {
			continue
		}
		if (s.Alias != nil && *s.Alias == name) || (s.Alias == nil && s.ImageName == name) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple owners shared %q with you; disambiguate with --from <owner-email>", name)
	}
	return &matches[0], nil
}

var imageSkillShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print an image's setup guide",
	Long: `Print the setup guide for one of your own images, or for an image shared
with you (accepted or still pending — reading the guide is part of deciding
whether to accept). Output is the raw markdown, so it pipes cleanly:

  chariot image skill show research > SKILL.md`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		name := args[0]

		// Your own image wins, mirroring deploy-name resolution.
		skill, err := client.GetImageSkill(cmd.Context(), name)
		if err == nil {
			fmt.Fprint(cmd.OutOrStdout(), skill.Content)
			return nil
		}
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			return err
		}

		shares, err := client.ListShares(cmd.Context())
		if err != nil {
			return err
		}
		share, err := findIncomingShare(shares, name, imageSkillFrom)
		if err != nil {
			return err
		}
		if share == nil {
			return fmt.Errorf("no setup guide for %q — not one of your images and no share by that name", name)
		}
		skill, err = client.GetShareSkill(cmd.Context(), share.ShareID)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), skill.Content)
		return nil
	},
}

var imageSkillSetCmd = &cobra.Command{
	Use:   "set <name> <file>",
	Short: "Attach or replace the setup guide on one of your images",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("reading %s: %w", args[1], err)
		}
		skill, err := client.SetImageSkill(cmd.Context(), args[0], string(content))
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ setup guide attached to %s (%d bytes)\n", skill.ImageName, len(content))
		fmt.Fprintln(out, "  Everyone you share the image with can read it with `chariot image skill show`.")
		return nil
	},
}

var imageSkillClearCmd = &cobra.Command{
	Use:   "clear <name>",
	Short: "Remove the setup guide from one of your images",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if err := client.ClearImageSkill(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ setup guide removed from %s\n", args[0])
		return nil
	},
}

func init() {
	imageSkillShowCmd.Flags().StringVar(&imageSkillFrom, "from", "", "owner email, when two owners shared the same name")
	imageSkillCmd.AddCommand(imageSkillShowCmd)
	imageSkillCmd.AddCommand(imageSkillSetCmd)
	imageSkillCmd.AddCommand(imageSkillClearCmd)
	imageCmd.AddCommand(imageSkillCmd)
}
