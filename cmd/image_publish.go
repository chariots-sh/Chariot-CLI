package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
)

var imagePublishDescription string

var imagePublishCmd = &cobra.Command{
	Use:   "publish <name>",
	Short: "List one of your verified custom images in the public catalog",
	Long: `List one of your verified custom images in the GLOBAL public catalog, so
any Chariot account can find it (` + "`chariot image browse`" + `) and add it
(` + "`chariot image add`" + `) without a private share from you.

A listing is a standing offer: adding it works exactly like accepting a
private share — the adder picks the name on their side and locks in the
current pod tier as their daily-fee ceiling; your re-pushes flow to them
automatically unless the tier rises, which needs their re-acceptance. Adders
pay for their own agents; you are never billed for theirs.

Take it back down with ` + "`chariot image unpublish <name>`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if err := client.PublishImage(cmd.Context(), args[0], imagePublishDescription); err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ %s is now public\n", args[0])
		fmt.Fprintln(out, "  Anyone can find it with `chariot image browse` and add it. Accounts that add")
		fmt.Fprintln(out, "  it appear in `chariot image shares`, where you can revoke them individually.")
		return nil
	},
}

var imageUnpublishCmd = &cobra.Command{
	Use:   "unpublish <name>",
	Short: "Remove your image from the public catalog",
	Long: `Remove your image from the public catalog. Stops discovery and new adds;
accounts that already added it keep their access until you revoke each one
(` + "`chariot image shares`" + ` lists them, ` + "`chariot image unshare <name> --with <email>`" + `
revokes one).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if err := client.UnpublishImage(cmd.Context(), args[0]); err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ %s is no longer public\n", args[0])
		fmt.Fprintln(out, "  Existing adds keep working until revoked — `chariot image shares` lists them.")
		return nil
	},
}

var imageBrowseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Browse the public image catalog",
	Long: `Browse custom images other accounts published. Add one to your account
with ` + "`chariot image add <name>`" + ` — it then deploys like any image
(` + "`chariot deploy --image <name>`" + `) and its owner's re-pushes flow to your
agents automatically (never at a bigger pod tier without your re-acceptance).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		listings, err := browseAllPublic(cmd, client)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(listings) == 0 {
			fmt.Fprintln(out, "No public images yet. Publish yours with `chariot image publish <name>`.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "IMAGE\tOWNER\tPOD SIZE\tDAILY FEE\tDESCRIPTION")
		for _, l := range listings {
			desc := ""
			if l.Description != nil {
				desc = *l.Description
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t$%.2f\t%s\n",
				l.ImageName, l.OwnerEmail, l.PodSize, l.DailyFeeDollars, desc)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(out, "\nAdd one with `chariot image add <name>` (--from <owner-email> if two owners")
		fmt.Fprintln(out, "publish the same name).")
		return nil
	},
}

var imageAddFrom string
var imageAddAlias string

var imageAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a public image to your account",
	Long: `Add an image from the public catalog (` + "`chariot image browse`" + `) to your
account. Works exactly like accepting a private share: you pick the name it
goes by on your side (--alias; defaults to its published name) and lock in
the current pod tier as your daily-fee ceiling — if the owner later
re-pushes at a bigger tier, the image stops resolving until you re-accept
(` + "`chariot image accept <name>`" + `), so your fees never rise without consent.

If two owners publish the same name, disambiguate with --from <owner-email>.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		listings, err := browseAllPublic(cmd, client)
		if err != nil {
			return err
		}
		name := args[0]
		var matches []api.PublicListing
		for _, l := range listings {
			if l.ImageName != name {
				continue
			}
			if imageAddFrom != "" && l.OwnerEmail != imageAddFrom {
				continue
			}
			matches = append(matches, l)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no public image named %q — `chariot image browse` lists the catalog", name)
		}
		if len(matches) > 1 {
			return fmt.Errorf("multiple owners publish %q; disambiguate with --from <owner-email>", name)
		}
		added, err := client.AddPublicImage(cmd.Context(), matches[0].OwnerEmail, name, imageAddAlias)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ added %s from %s (pod size %s)\n",
			added.Alias, matches[0].OwnerEmail, added.AcceptedPodSize)
		fmt.Fprintf(out, "  Deploy with `chariot deploy --image %s`. The owner's re-pushes flow to your\n", added.Alias)
		fmt.Fprintln(out, "  agents automatically — unless the pod tier rises, which needs your re-acceptance.")
		return nil
	},
}

// browseAllPublic walks every page of the public catalog. The catalog is
// bounded by how many customers publish images, so client-side pagination
// walking is fine at current scale.
func browseAllPublic(cmd *cobra.Command, client *api.Client) ([]api.PublicListing, error) {
	var all []api.PublicListing
	cursor := ""
	for {
		page, err := client.BrowsePublic(cmd.Context(), cursor, 200)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Listings...)
		if page.NextCursor == nil {
			return all, nil
		}
		cursor = *page.NextCursor
	}
}

func init() {
	imagePublishCmd.Flags().StringVar(&imagePublishDescription, "description", "", "short blurb shown in the public catalog")
	imageAddCmd.Flags().StringVar(&imageAddFrom, "from", "", "owner email, when two owners publish the same name")
	imageAddCmd.Flags().StringVar(&imageAddAlias, "alias", "", "name the image goes by on your side (default: its name)")
	imageCmd.AddCommand(imagePublishCmd)
	imageCmd.AddCommand(imageUnpublishCmd)
	imageCmd.AddCommand(imageBrowseCmd)
	imageCmd.AddCommand(imageAddCmd)
}
