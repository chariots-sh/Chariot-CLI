package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var imageShareWith string
var imageShareAlias string

var imageShareCmd = &cobra.Command{
	Use:   "share <name> --with <email>",
	Short: "Share one of your verified custom images with another account",
	Long: `Share one of your verified custom images with another Chariot account.

The other account deploys it like any catalog image — ` + "`chariot deploy --image <name>`" + ` —
and re-pushes of your image flow to their agents automatically (adopted at
each agent's next wake, exactly like your own fleet). They are billed for
their own agents; you are never billed for theirs.

By default the image keeps its name on their side. If that name is taken in
their account, pass --alias to pick the name they will deploy it by.

Revoke any time with ` + "`chariot image unshare <name> --with <email>`" + ` — their
agents fall back to their default image at the next wake.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if imageShareWith == "" {
			return fmt.Errorf("--with <email> is required")
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		share, err := client.CreateShare(cmd.Context(), args[0], imageShareWith, imageShareAlias)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ shared %s with %s", share.ImageName, share.GranteeEmail)
		if share.Alias != share.ImageName {
			fmt.Fprintf(out, " (as %s)", share.Alias)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  They can now run `chariot deploy --image %s`. Re-pushes of %s flow to them automatically.\n",
			share.Alias, share.ImageName)
		return nil
	},
}

var imageSharesCmd = &cobra.Command{
	Use:   "shares",
	Short: "List images you've shared and images shared with you",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		shares, err := client.ListShares(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(shares.Outgoing) == 0 && len(shares.Incoming) == 0 {
			fmt.Fprintln(out, "No image shares. Share one with `chariot image share <name> --with <email>`.")
			return nil
		}
		if len(shares.Outgoing) > 0 {
			fmt.Fprintln(out, "SHARED BY YOU")
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "IMAGE\tWITH\tTHEIR NAME")
			for _, s := range shares.Outgoing {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.ImageName, s.GranteeEmail, s.Alias)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
		}
		if len(shares.Incoming) > 0 {
			if len(shares.Outgoing) > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, "SHARED WITH YOU")
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "IMAGE\tFROM\tPOD SIZE\tSTATUS")
			for _, s := range shares.Incoming {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					s.Alias, s.OwnerEmail, orDash(s.PodSize), incomingStatus(s.Ready))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "\nRemove one with `chariot image unshare <name>` (yours: add --with <email>).")
		return nil
	},
}

var imageUnshareWith string

var imageUnshareCmd = &cobra.Command{
	Use:   "unshare <name>",
	Short: "Revoke a share you granted, or remove an image shared with you",
	Long: `Revoke a share you granted (` + "`chariot image unshare <name> --with <email>`" + `),
or remove an image someone shared with you (` + "`chariot image unshare <name>`" + `,
where <name> is the name it appears under in ` + "`chariot images`" + `).

Agents still deployed onto the name fall back to the default image at their
next wake — they keep running until then.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		shares, err := client.ListShares(cmd.Context())
		if err != nil {
			return err
		}
		name := args[0]
		out := cmd.OutOrStdout()

		if imageUnshareWith != "" { // owner revoking a grant
			for _, s := range shares.Outgoing {
				if s.ImageName != name || s.GranteeEmail != imageUnshareWith {
					continue
				}
				if err := client.DeleteShare(cmd.Context(), s.ShareID); err != nil {
					return err
				}
				fmt.Fprintf(out, "✓ %s is no longer shared with %s\n", name, s.GranteeEmail)
				fmt.Fprintln(out, "  Their agents fall back to their default image at the next wake.")
				return nil
			}
			return fmt.Errorf("no share of %q with %s — `chariot image shares` lists them", name, imageUnshareWith)
		}

		for _, s := range shares.Incoming { // grantee removing a received share
			if s.Alias != name {
				continue
			}
			if err := client.DeleteShare(cmd.Context(), s.ShareID); err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ removed %s (shared by %s)\n", s.Alias, s.OwnerEmail)
			fmt.Fprintln(out, "  Agents still deployed onto it fall back to your default image at the next wake.")
			return nil
		}
		return fmt.Errorf("no image shared with you named %q — to revoke a share YOU granted, add --with <email>", name)
	},
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func incomingStatus(ready bool) string {
	if ready {
		return "available"
	}
	return "unavailable (owner re-pushing)"
}

func init() {
	imageShareCmd.Flags().StringVar(&imageShareWith, "with", "", "email of the account to share with (required)")
	imageShareCmd.Flags().StringVar(&imageShareAlias, "alias", "", "name the image goes by on their side (default: its name)")
	imageUnshareCmd.Flags().StringVar(&imageUnshareWith, "with", "", "revoke your grant to this email (omit to remove an image shared with you)")
	imageCmd.AddCommand(imageShareCmd)
	imageCmd.AddCommand(imageSharesCmd)
	imageCmd.AddCommand(imageUnshareCmd)
}
