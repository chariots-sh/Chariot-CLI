package cmd

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var (
	fleetCreateImages      []string
	fleetCreateDescription string
	fleetDeleteYes         bool
)

var fleetCmd = &cobra.Command{
	Use:   "fleet",
	Short: "Create and share fleet packs — deployable bundles of agent images",
	Long: `Create and share fleet packs.

A fleet pack is a named, shareable fleet recipe: images with per-image agent
counts — built-ins and/or your verified custom images — plus an optional setup
skill (a markdown guide for whoever deploys it, human or coding agent). A
shared single image is simply a pack of one.

  chariot fleet create quant-firm --image zeroclaw:5 --image my-brain:1
  chariot fleet skill set quant-firm SETUP.md   # attach the setup skill
  chariot fleet publish quant-firm              # list it in the public catalog
  chariot fleet browse                          # what others published
  chariot deploy-fleet quant-firm --from owner@example.com

Publishing is a standing offer to every account: deploying your pack shares
its custom images with the deployer (at the current pod tier — their fee
ceiling until you re-push at a higher one). Unpublishing stops new deploys;
fleets already deployed keep running.`,
}

// parseFleetImageSpec parses one --image value: "<name>" or "<name>:<count>".
func parseFleetImageSpec(raw string) (api.FleetPackItemSpec, error) {
	name, countRaw, found := strings.Cut(raw, ":")
	if name == "" {
		return api.FleetPackItemSpec{}, fmt.Errorf("empty image name in --image %q", raw)
	}
	if !found {
		return api.FleetPackItemSpec{ImageName: name, Count: 1}, nil
	}
	count, err := strconv.Atoi(countRaw)
	if err != nil || count < 1 {
		return api.FleetPackItemSpec{}, fmt.Errorf(
			"--image %q: count must be a positive integer (e.g. %s:3)", raw, name)
	}
	return api.FleetPackItemSpec{ImageName: name, Count: count}, nil
}

var fleetCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a pack (or replace an existing one's images)",
	Long: `Create a fleet pack, or replace an existing pack's images and description
wholesale — publication state and the setup skill survive an edit.

Each --image is a built-in name or one of your verified custom images, with an
optional agent count (default 1):

  chariot fleet create quant-firm --image zeroclaw:5 --image my-brain:1 \
    --description "runs a quant firm"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(fleetCreateImages) == 0 {
			return fmt.Errorf("give the pack at least one --image <name>[:<count>]")
		}
		items := make([]api.FleetPackItemSpec, 0, len(fleetCreateImages))
		for _, raw := range fleetCreateImages {
			spec, err := parseFleetImageSpec(raw)
			if err != nil {
				return err
			}
			items = append(items, spec)
		}
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		pack, err := client.CreateFleetPack(cmd.Context(), args[0], fleetCreateDescription, items)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ pack %s: %s (%d agents)\n", pack.Name, fleetItemsSummary(pack.Items), pack.TotalAgents)
		if !pack.HasSkill {
			fmt.Fprintln(out, "  Attach a setup skill with `chariot fleet skill set` — it walks deployers (and their coding agents) through wiring the fleet up.")
		}
		if !pack.Published {
			fmt.Fprintf(out, "  Publish it with `chariot fleet publish %s`; deploy it yourself with `chariot deploy-fleet %s`.\n", pack.Name, pack.Name)
		}
		return nil
	},
}

var fleetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your fleet packs",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		packs, err := client.ListFleetPacks(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(packs.Packs) == 0 {
			fmt.Fprintln(out, "No fleet packs yet. Create one with `chariot fleet create <name> --image <image>:<count>`.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PACK\tIMAGES\tAGENTS\tSKILL\tVISIBILITY\tDEPLOYS")
		for _, pack := range packs.Packs {
			visibility := "private"
			if pack.Published {
				visibility = "public"
			}
			skill := "-"
			if pack.HasSkill {
				skill = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%d\n",
				pack.Name, fleetItemsSummary(pack.Items), pack.TotalAgents, skill, visibility, pack.DeployCount)
		}
		return tw.Flush()
	},
}

var fleetDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a pack (fleets already deployed from it keep running)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if !fleetDeleteYes {
			fmt.Fprintf(cmd.OutOrStdout(), "Delete pack %s? Fleets already deployed from it keep running. [y/N] ", args[0])
			reader := bufio.NewReader(cmd.InOrStdin())
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		if err := client.DeleteFleetPack(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ deleted pack %s\n", args[0])
		return nil
	},
}

var fleetPublishCmd = &cobra.Command{
	Use:   "publish <name>",
	Short: "List a pack in the public catalog",
	Long: `List a pack in the public catalog — a standing offer any account can deploy
with ` + "`chariot deploy-fleet`" + `.

Publishing implies consent to share the pack's custom images with every
account that deploys it (each deploy binds a share at the image's current pod
tier — that deployer's fee ceiling until you re-push at a higher one).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		pack, err := client.PublishFleetPack(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ %s is public — anyone can deploy it:\n", pack.Name)
		fmt.Fprintf(out, "    chariot deploy-fleet %s --from <your-email>\n", pack.Name)
		return nil
	},
}

var fleetUnpublishCmd = &cobra.Command{
	Use:   "unpublish <name>",
	Short: "Remove a pack from the public catalog",
	Long: `Remove a pack from the public catalog: stops discovery and new deploys.
Fleets already deployed from it — and the image shares those deploys bound —
keep working until torn down / revoked individually (manage shares on the web
account page).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		if err := client.UnpublishFleetPack(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s is no longer public\n", args[0])
		return nil
	},
}

var fleetBrowseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Browse the public fleet-pack catalog",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		packs, err := browseAllFleetPacks(cmd, client)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(packs) == 0 {
			fmt.Fprintln(out, "No public fleet packs yet. Publish yours with `chariot fleet publish <name>`.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PACK\tOWNER\tIMAGES\tAGENTS\tDAILY FEE\tSKILL\tDEPLOYS\tDESCRIPTION")
		for _, pack := range packs {
			skill := "-"
			if pack.HasSkill {
				skill = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t$%.2f\t%s\t%d\t%s\n",
				pack.Name, pack.OwnerEmail, fleetItemsSummary(pack.Items),
				pack.TotalAgents, pack.TotalDailyFeeDollars, skill,
				pack.DeployCount, orDash(pack.Description))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(out, "\nDeploy one with `chariot deploy-fleet <pack> --from <owner-email>`.")
		return nil
	},
}

// fleetItemsSummary renders a pack's composition on one line: "5× zeroclaw + 1× brain".
func fleetItemsSummary(items []api.FleetPackItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%d× %s", item.Count, item.ImageName))
	}
	return strings.Join(parts, " + ")
}

// browseAllFleetPacks walks every page of the public pack catalog. Bounded by
// how many customers publish packs, so client-side walking is fine at scale.
func browseAllFleetPacks(cmd *cobra.Command, client *api.Client) ([]api.PublicFleetPack, error) {
	var all []api.PublicFleetPack
	cursor := ""
	for {
		page, err := client.BrowseFleetPacks(cmd.Context(), cursor, 200)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Packs...)
		if page.NextCursor == nil {
			return all, nil
		}
		cursor = *page.NextCursor
	}
}

func init() {
	fleetCreateCmd.Flags().StringArrayVar(&fleetCreateImages, "image", nil,
		"image and count, as <name>[:<count>] — repeat per image (built-in or your custom name)")
	fleetCreateCmd.Flags().StringVar(&fleetCreateDescription, "description", "",
		"short blurb shown in the public catalog")
	fleetDeleteCmd.Flags().BoolVarP(&fleetDeleteYes, "yes", "y", false, "skip the confirmation prompt")

	fleetCmd.AddCommand(fleetCreateCmd)
	fleetCmd.AddCommand(fleetListCmd)
	fleetCmd.AddCommand(fleetDeleteCmd)
	fleetCmd.AddCommand(fleetPublishCmd)
	fleetCmd.AddCommand(fleetUnpublishCmd)
	fleetCmd.AddCommand(fleetBrowseCmd)
	rootCmd.AddCommand(fleetCmd)
}
