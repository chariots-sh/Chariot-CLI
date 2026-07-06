package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/Immortal-Protocols/Chariot-CLI/internal/sshsession"
)

const defaultSSHHost = "ssh.chariots.sh"

var (
	sshHost   string
	sshPort   int
	sshConfig bool
)

var sshCmd = &cobra.Command{
	Use:   "ssh <agent> [-- command...]",
	Short: "Open an interactive shell inside one of your agents",
	Long: `Open an SSH session inside one of your agents.

You land in the agent's own container — its files, tools, and workspace at
/zeroclaw-data. Authentication uses a short-lived certificate Chariot issues
for you; there are no keys to manage and nothing about the underlying
infrastructure is exposed.

    chariot ssh my-agent-3
    chariot ssh my-agent-3 -- cat /zeroclaw-data/workspace/MEMORY.md

Run 'chariot ssh --config <agent>' to print a ~/.ssh/config block so that plain
ssh, scp, and tools like VS Code Remote work against that agent.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSSH,
}

func runSSH(cmd *cobra.Command, args []string) error {
	client, _, err := authedClient()
	if err != nil {
		return err
	}
	mgr, err := newSSHManager()
	if err != nil {
		return err
	}

	if sshConfig {
		if len(args) == 0 {
			return fmt.Errorf("which agent? e.g. `chariot ssh --config my-agent-3`")
		}
		return printSSHConfig(cmd, client, mgr, args[0])
	}

	if len(args) == 0 {
		return fmt.Errorf("which agent? e.g. `chariot ssh my-agent-3` (or `chariot ssh --config my-agent-3`)")
	}
	slug, remoteCmd := args[0], args[1:]

	creds, err := mgr.Ensure(cmd.Context(), client)
	if err != nil {
		return err
	}

	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("no `ssh` client found on PATH — install OpenSSH")
	}
	argv := sshsession.SSHArgs(creds, sshHost, slug, sshPort, remoteCmd)

	// Hand the terminal fully to ssh; propagate its exit code.
	c := exec.Command(sshBin, argv...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	return nil
}

// printSSHConfig writes a concrete ~/.ssh/config Host block for one agent, so
// plain ssh/scp/rsync/VS Code Remote work against it (e.g. `scp f my-agent-3:`).
// It mints credentials first so the pinned known_hosts + a valid cert exist
// before the user wires this in. The cert is short-lived — re-run this (or any
// `chariot ssh`) to refresh it.
func printSSHConfig(cmd *cobra.Command, client *api.Client, mgr *sshsession.Manager, slug string) error {
	creds, err := mgr.Ensure(cmd.Context(), client)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, `# Add to ~/.ssh/config, then: ssh %[1]s / scp file %[1]s: / VS Code Remote host "%[1]s".
# The certificate is short-lived; refresh it with: chariot ssh --config %[1]s
Host %[1]s
    HostName %[2]s
    Port %[3]d
    User %[1]s
    IdentityFile %[4]s
    CertificateFile %[5]s
    IdentitiesOnly yes
    UserKnownHostsFile %[6]s
    StrictHostKeyChecking yes
`, slug, sshHost, sshPort, creds.KeyPath, creds.CertPath, creds.KnownHostsPath)
	return nil
}

func newSSHManager() (*sshsession.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return sshsession.NewManager(filepath.Join(home, ".chariot", "ssh"), sshHost), nil
}

func init() {
	sshCmd.Flags().StringVar(&sshHost, "host", defaultSSHHost, "SSH gateway hostname")
	sshCmd.Flags().IntVar(&sshPort, "port", 22, "SSH gateway port (use 443 on networks that block 22)")
	sshCmd.Flags().BoolVar(&sshConfig, "config", false, "print a ~/.ssh/config block for ssh/scp/VS Code and exit")
	rootCmd.AddCommand(sshCmd)
}
