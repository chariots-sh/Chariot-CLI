package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
)

var imageShareWith string

var imageShareCmd = &cobra.Command{
	Use:   "share <name> --with <email>",
	Short: "Offer one of your verified custom images to another account",
	Long: `Offer one of your verified custom images to another Chariot account.

Nothing changes on their side until they accept the offer with
` + "`chariot image accept`" + ` — acceptance is what binds the name they deploy it
by and the pod tier (daily fee) they agree to. Once accepted, they deploy it
like any catalog image (` + "`chariot deploy --image <name>`" + `), and re-pushes of
your image flow to their agents automatically at each agent's next wake —
unless a re-push raises the pod tier, which stops resolving until they
re-accept (their fees can never rise without their consent). They are billed
for their own agents; you are never billed for theirs.

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
		share, err := client.CreateShare(cmd.Context(), args[0], imageShareWith)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ offered %s to %s\n", share.ImageName, share.GranteeEmail)
		fmt.Fprintf(out, "  Pending their acceptance — they run `chariot image accept %s` to use it.\n",
			share.ImageName)
		return nil
	},
}

var imageAcceptAlias string
var imageAcceptFrom string

var imageAcceptCmd = &cobra.Command{
	Use:   "accept <name>",
	Short: "Accept an image shared with you (or approve its tier raise)",
	Long: `Accept an image another account offered you (` + "`chariot image shares`" + ` lists
pending offers). Acceptance binds the name YOU deploy it by (--alias; defaults
to the owner's image name) and locks in the current pod tier as the daily-fee
ceiling: if the owner later re-pushes at a bigger tier, the image stops
resolving until you accept again, so your fees never rise without consent.

<name> matches the offer's image name (for pending offers) or your alias (to
re-accept a tier raise). If two owners offered images with the same name,
disambiguate with --from <owner-email>.`,
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
		var matches []api.IncomingShare
		for _, s := range shares.Incoming {
			if imageAcceptFrom != "" && s.OwnerEmail != imageAcceptFrom {
				continue
			}
			if (s.Alias != nil && *s.Alias == name) || (s.Alias == nil && s.ImageName == name) {
				matches = append(matches, s)
			}
		}
		if len(matches) == 0 {
			return fmt.Errorf("no image shared with you named %q — `chariot image shares` lists offers", name)
		}
		if len(matches) > 1 {
			return fmt.Errorf("multiple owners shared %q with you; disambiguate with --from <owner-email>", name)
		}
		accepted, err := client.AcceptShare(cmd.Context(), matches[0].ShareID, imageAcceptAlias)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ accepted %s from %s (pod size %s)\n",
			accepted.Alias, matches[0].OwnerEmail, accepted.AcceptedPodSize)
		fmt.Fprintf(out, "  Deploy with `chariot deploy --image %s`. Their re-pushes flow to your agents\n", accepted.Alias)
		fmt.Fprintln(out, "  automatically — unless the pod tier rises, which needs your re-acceptance.")
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
			fmt.Fprintln(out, "No image shares. Offer one with `chariot image share <name> --with <email>`.")
			return nil
		}
		if len(shares.Outgoing) > 0 {
			fmt.Fprintln(out, "SHARED BY YOU")
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "IMAGE\tWITH\tTHEIR NAME\tSTATUS")
			for _, s := range shares.Outgoing {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					s.ImageName, s.GranteeEmail, orDash(s.Alias), shareStatusText(s.Status))
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
				name := s.ImageName
				if s.Alias != nil {
					name = *s.Alias
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					name, s.OwnerEmail, orDash(s.PodSize), shareStatusText(s.Status))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "\nAccept an offer with `chariot image accept <name>`; remove one with")
		fmt.Fprintln(out, "`chariot image unshare <name>` (yours: add --with <email>).")
		return nil
	},
}

var imageUnshareWith string

var imageUnshareCmd = &cobra.Command{
	Use:   "unshare <name>",
	Short: "Revoke a share you granted, or remove/decline one shared with you",
	Long: `Revoke a share you granted (` + "`chariot image unshare <name> --with <email>`" + `),
or remove an image someone shared with you — accepted or still pending —
(` + "`chariot image unshare <name>`" + `, where <name> is the name it appears under
in ` + "`chariot image shares`" + `).

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

		if imageUnshareWith != "" { // owner revoking a grant/offer
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

		for _, s := range shares.Incoming { // grantee removing/declining
			if !((s.Alias != nil && *s.Alias == name) || (s.Alias == nil && s.ImageName == name)) {
				continue
			}
			if err := client.DeleteShare(cmd.Context(), s.ShareID); err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ removed %s (shared by %s)\n", name, s.OwnerEmail)
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

// shareStatusText renders a share's lifecycle status for humans. The zero
// coupling to fmt strings elsewhere is deliberate: `images` and `image
// shares` must describe the same state the same way.
func shareStatusText(status string) string {
	switch status {
	case "pending":
		return "pending — `chariot image accept`"
	case "active":
		return "available"
	case "owner_repushing":
		return "unavailable (owner re-pushing)"
	case "tier_raised":
		return "needs re-accept (pod tier raised)"
	}
	return status
}

func init() {
	imageShareCmd.Flags().StringVar(&imageShareWith, "with", "", "email of the account to share with (required)")
	imageAcceptCmd.Flags().StringVar(&imageAcceptAlias, "alias", "", "name the image goes by on your side (default: its name)")
	imageAcceptCmd.Flags().StringVar(&imageAcceptFrom, "from", "", "owner email, when two owners offered the same name")
	imageUnshareCmd.Flags().StringVar(&imageUnshareWith, "with", "", "revoke your grant to this email (omit to remove an image shared with you)")
	imageCmd.AddCommand(imageShareCmd)
	imageCmd.AddCommand(imageAcceptCmd)
	imageCmd.AddCommand(imageSharesCmd)
	imageCmd.AddCommand(imageUnshareCmd)
}
