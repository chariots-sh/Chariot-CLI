// Package cmd holds the Chariot CLI commands.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/chariots-sh/Chariot-CLI/internal/config"
	"github.com/chariots-sh/Chariot-CLI/internal/update"
	"github.com/spf13/cobra"
)

// disableAutoUpdateCheck skips the background update notice. Tests set this
// so they never make a real network call.
var disableAutoUpdateCheck bool

// updateNoticeSkip lists leaf commands that shouldn't get an update notice
// tacked onto their output: `update` and `version` already talk about
// versions, and `completion` output is meant to be sourced by a shell.
var updateNoticeSkip = map[string]bool{
	"update":     true,
	"version":    true,
	"completion": true,
}

var rootCmd = &cobra.Command{
	Use:   "chariot",
	Short: "Chariot — deploy and manage enterprise agent fleets",
	Long: `Chariot CLI — deploys and manages agent fleets. Messaging agents in
production is done by your own service via the HTTP API, not the CLI.

Typical flow:
  chariot login                                  # authenticate (opens browser)
  chariot deploy --count 10000 --endpoint URL    # spin up a fleet
  chariot list                                   # see your agents + their ids
  chariot api                                    # HTTP API your service integrates against

Smoke-test the round-trip once, before writing code (not a production interface):
  chariot demo send <agent-id> "hello"           # message an agent (token-seed auth)
  chariot demo watch                             # poll the reply inbox`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		notifyIfUpdateAvailable(cmd)
	},
}

// Execute runs the CLI, printing errors and setting a non-zero exit code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// notifyIfUpdateAvailable prints a one-line hint to stderr when a newer
// release is already known (from this or a prior run's background check). It
// never blocks a command by more than updateCheckWait: see
// update.CheckInBackground.
const updateCheckWait = 250 * time.Millisecond

func notifyIfUpdateAvailable(cmd *cobra.Command) {
	if disableAutoUpdateCheck || Version == "dev" || updateNoticeSkip[cmd.Name()] {
		return
	}
	latest := update.CheckInBackground(Version, updateCheckWait)
	if latest == "" {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "\nA new version of chariot is available: %s (you have %s). Run `chariot update` to install it.\n", latest, Version)
}

// loadConfig loads the on-disk config (or an empty one).
func loadConfig() (*config.Config, error) {
	return config.Load()
}

// saveConfig persists the config.
func saveConfig(cfg *config.Config) error {
	return config.Save(cfg)
}

// authedClient builds an API client using the stored session token, erroring
// with a friendly hint when the user isn't logged in.
func authedClient() (*api.Client, *config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg.Token == "" {
		return nil, nil, fmt.Errorf("not logged in — run `chariot login` first")
	}
	return api.New(cfg.BaseURL(), cfg.Token), cfg, nil
}
