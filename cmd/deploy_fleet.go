package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var (
	deployFleetFrom     string
	deployFleetEndpoint string
	deployFleetModel    string
	deployFleetYes      bool
)

var deployFleetCmd = &cobra.Command{
	Use:   "deploy-fleet <pack>",
	Short: "Deploy a whole fleet pack in one shot",
	Long: `Deploy a whole fleet pack in one shot: every image's agents, one token-seed.

  chariot deploy-fleet quant-firm                        # your own pack
  chariot deploy-fleet quant-firm --from max@example.com # a published pack
  chariot deploy-fleet zeroclaw                          # a built-in, as a pack of one

Deploying another account's pack shows its composition and daily fees first —
confirming consents to those fees, and binds each custom image to your account
as a share (the owner's re-pushes flow to your fleet unless the pod tier, and
so the fee, rises). --endpoint and --model pass through exactly like
` + "`chariot deploy`" + `.

If the pack ships a setup skill, the deploy ends in a menu: run the skill with
claude code or codex (a handoff file with the deploy context and token-seed is
written to the current directory), display it, or quit.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client, cfg, err := authedClient()
		if err != nil {
			return err
		}

		// ONE buffered reader for the whole flow: a second bufio.Reader
		// would lose whatever the first one read ahead, so the confirm
		// prompt would swallow the menu's input.
		reader := bufio.NewReader(cmd.InOrStdin())

		if !deployFleetYes {
			confirmed, err := confirmFleetDeploy(cmd, reader, client, name)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

		res, err := client.DeployFleetPack(cmd.Context(), name, deployFleetFrom, deployFleetEndpoint, deployFleetModel)
		if err != nil {
			return err
		}
		printFleetDeployResult(cmd, res)

		if res.SkillContent == nil {
			return nil
		}
		return runSkillMenu(cmd, reader, res, cfg.BaseURL())
	},
}

// confirmFleetDeploy shows what the deploy creates — and, for a foreign pack,
// the daily fees being consented to — then reads a y/N answer.
func confirmFleetDeploy(cmd *cobra.Command, reader *bufio.Reader, client *api.Client, name string) (bool, error) {
	out := cmd.OutOrStdout()
	if deployFleetFrom != "" {
		pack, err := client.GetPublicFleetPack(cmd.Context(), deployFleetFrom, name)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(out, "\nPack %s by %s", pack.Name, pack.OwnerEmail)
		if pack.Description != nil {
			fmt.Fprintf(out, " — %s", *pack.Description)
		}
		fmt.Fprintln(out)
		for _, item := range pack.Items {
			fmt.Fprintf(out, "  %d× %-20s %s pod · $%.2f/day per agent\n",
				item.Count, item.ImageName, orDash(item.PodSize), orZero(item.DailyFeeDollars))
		}
		fmt.Fprintf(out, "  = %d agents · $%.2f/day once fully active (agents cost nothing until messaged)\n",
			pack.TotalAgents, pack.TotalDailyFeeDollars)
		if pack.HasSkill {
			fmt.Fprintln(out, "  Ships a setup skill — offered after the deploy.")
		}
		fmt.Fprintln(out, "  Deploying binds the pack's custom images to your account at these tiers (your fee ceiling).")
	} else {
		// Own pack, or a builtin template name — the backend decides; just
		// name what is about to happen.
		fmt.Fprintf(out, "\nDeploying %s.\n", name)
	}
	fmt.Fprint(out, "Continue? [y/N] ")
	line, _ := reader.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y", nil
}

func printFleetDeployResult(cmd *cobra.Command, res *api.FleetDeployResult) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n✓ %d agents live (total %d)\n\n", res.Created, res.Total)
	for _, group := range res.Groups {
		label := group.ImageName
		if group.DeployName != group.ImageName {
			label = fmt.Sprintf("%s (as %s)", group.ImageName, group.DeployName)
		}
		fmt.Fprintf(out, "  %-24s ×%-4d %s\n", label, group.Count, slugRange(group.Slugs))
	}
	if deployFleetEndpoint != "" {
		fmt.Fprintf(out, "\n  endpoint    : %s\n", deployFleetEndpoint)
	} else {
		fmt.Fprintln(out, "\n  endpoint    : none — replies go to the inbox (`chariot demo watch`)")
	}
	fmt.Fprintf(out, "  model       : %s\n", res.Model)
	fmt.Fprintf(out, "  namespace   : %s\n", res.Namespace)
	fmt.Fprintf(out, "  token-seed  : %s\n", res.TokenSeed)
	fmt.Fprintln(out, "\n  Save the token-seed now — it is shown only once.")
	fmt.Fprintln(out, "  Your service messages agents over the HTTP API:")
	fmt.Fprintln(out, "    POST {api-base}/v1/agents/{agent-id}/messages")
	fmt.Fprintln(out, "    header  X-Chariot-Token: "+res.TokenSeed)
	fmt.Fprintln(out, "  Full API reference: `chariot api` · https://app.chariots.sh/docs")
}

// slugRange renders a group's slugs compactly: "agent-000003 … agent-000007"
// (slugs are contiguous per group).
func slugRange(slugs []string) string {
	if len(slugs) == 0 {
		return ""
	}
	if len(slugs) == 1 {
		return slugs[0]
	}
	return slugs[0] + " … " + slugs[len(slugs)-1]
}

// runSkillMenu is the post-deploy loop: run the setup skill with a coding
// agent, display it (then come back), or quit.
func runSkillMenu(cmd *cobra.Command, reader *bufio.Reader, res *api.FleetDeployResult, apiURL string) error {
	out := cmd.OutOrStdout()
	for {
		fmt.Fprintln(out, "\nThis pack ships a setup skill that walks a coding agent through wiring the fleet up.")
		fmt.Fprintln(out, "  1) run it with claude code")
		fmt.Fprintln(out, "  2) run it with codex")
		fmt.Fprintln(out, "  3) display it")
		fmt.Fprintln(out, "  q) quit")
		fmt.Fprint(out, "> ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return nil // EOF — non-interactive caller
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "1", "claude":
			if err := launchCodingAgent(cmd, "claude", res, apiURL); err != nil {
				fmt.Fprintf(out, "  %v\n", err)
				continue // back to the menu
			}
			return nil
		case "2", "codex":
			if err := launchCodingAgent(cmd, "codex", res, apiURL); err != nil {
				fmt.Fprintf(out, "  %v\n", err)
				continue // back to the menu
			}
			return nil
		case "3", "d", "display":
			fmt.Fprintln(out, "\n"+strings.TrimRight(*res.SkillContent, "\n"))
			// Loop back: after reading, choose to run it or quit.
		case "q", "quit", "exit":
			return nil
		default:
			fmt.Fprintln(out, "  Pick 1, 2, 3, or q.")
		}
	}
}

// launchCodingAgent writes the handoff file and hands the terminal to the
// coding agent with a prompt pointing at it. A missing binary is a soft error
// — the caller returns to the menu.
func launchCodingAgent(cmd *cobra.Command, bin string, res *api.FleetDeployResult, apiURL string) error {
	path, err := writeFleetHandoffFile(res, apiURL)
	if err != nil {
		return err
	}
	binPath, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("no `%s` found on PATH — install it, or display the skill instead (handoff file written to %s)", bin, path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n  Handoff file: %s (contains the token-seed — delete it when setup is done)\n", path)
	prompt := fmt.Sprintf(
		"Read the file at %s — it contains the deploy context and a setup skill for the Chariot agent fleet that was just deployed. Follow the skill to set the fleet up.",
		path,
	)
	// Hand the terminal fully to the coding agent; propagate its exit code.
	c := exec.Command(binPath, prompt)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	return nil
}

// writeFleetHandoffFile writes the setup skill plus everything a coding agent
// needs to act on it — API base URL, namespace, agents grouped by image, and
// the token-seed — to a 0600 file in the current directory, and returns its
// absolute path.
func writeFleetHandoffFile(res *api.FleetDeployResult, apiURL string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Chariot fleet setup — %s\n\n", res.PackName)
	b.WriteString("> ⚠ This file contains the fleet's token-seed. Delete it once setup is done.\n\n")
	b.WriteString("## Deploy context\n\n")
	fmt.Fprintf(&b, "- API base URL: %s\n", apiURL)
	fmt.Fprintf(&b, "- Namespace: %s\n", res.Namespace)
	if res.OwnerEmail != nil {
		fmt.Fprintf(&b, "- Pack: %s (by %s)\n", res.PackName, *res.OwnerEmail)
	} else {
		fmt.Fprintf(&b, "- Pack: %s\n", res.PackName)
	}
	fmt.Fprintf(&b, "- Model: %s\n", res.Model)
	fmt.Fprintf(&b, "- Token-seed (send as the `X-Chariot-Token` header): %s\n", res.TokenSeed)
	b.WriteString("- Agents (deactivated until first messaged; message one with `POST {api-base}/v1/agents/{agent-slug}/messages`):\n")
	for _, group := range res.Groups {
		label := group.ImageName
		if group.DeployName != group.ImageName {
			label = fmt.Sprintf("%s (deployed as %s)", group.ImageName, group.DeployName)
		}
		fmt.Fprintf(&b, "  - %s ×%d (%s pod): %s\n", label, group.Count, group.PodSize, strings.Join(group.Slugs, ", "))
	}
	b.WriteString("\n## Setup skill\n\n")
	b.WriteString(strings.TrimRight(*res.SkillContent, "\n"))
	b.WriteString("\n")

	name := fmt.Sprintf("chariot-fleet-%s-setup.md", res.PackName)
	if err := os.WriteFile(name, []byte(b.String()), 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", name, err)
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return name, nil
	}
	return abs, nil
}

// orZero renders an optional fee.
func orZero(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func init() {
	deployFleetCmd.Flags().StringVar(&deployFleetFrom, "from", "",
		"owner email — deploy that account's published pack instead of your own")
	deployFleetCmd.Flags().StringVar(&deployFleetEndpoint, "endpoint", "",
		"webhook URL your agents reply to (optional; omit for inbox-only)")
	deployFleetCmd.Flags().StringVar(&deployFleetModel, "model", "",
		"model the created agents run (optional; any OpenRouter model id)")
	deployFleetCmd.Flags().BoolVarP(&deployFleetYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(deployFleetCmd)
}
