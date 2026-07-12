package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/chariots-sh/Chariot-CLI/internal/update"
)

var updateCheckOnly bool

// executablePath resolves the running binary's real path. Overridden in
// tests so an update-flow test installs over a throwaway file instead of the
// actual test binary.
var executablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the CLI to the latest release",
	Long: `Checks GitHub for the latest chariot release and, if it's newer than the
version you're running, downloads and installs it in place.

Installed via Homebrew? This defers to ` + "`brew upgrade chariot`" + ` instead
of self-replacing the binary, so brew's own bookkeeping stays correct.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only check for a newer version, don't install it")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()

	if Version == "dev" {
		fmt.Fprintln(out, "chariot dev — this is an unversioned local build, skipping the update check.")
		return nil
	}

	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	if update.InstallMethod(exe) == "brew" {
		fmt.Fprintln(out, "chariot was installed via Homebrew — run `brew upgrade chariot` to update it.")
		return nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	rel, err := update.FetchLatest(cmd.Context(), client)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	if !update.IsNewer(Version, rel.TagName) {
		fmt.Fprintf(out, "chariot is up to date (%s).\n", Version)
		return nil
	}

	if updateCheckOnly {
		fmt.Fprintf(out, "a new version is available: %s (you have %s) — run `chariot update` to install it.\n", rel.TagName, Version)
		return nil
	}

	fmt.Fprintf(out, "updating chariot %s -> %s ...\n", Version, rel.TagName)
	if err := update.Apply(cmd.Context(), client, rel, exe); err != nil {
		return fmt.Errorf("installing update: %w", err)
	}
	fmt.Fprintf(out, "updated to %s. Run `chariot version` to confirm.\n", rel.TagName)
	return nil
}
