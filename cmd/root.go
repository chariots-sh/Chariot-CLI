// Package cmd holds the Chariot CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/api"
	"github.com/Immortal-Protocols/Chariot-CLI/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "chariot",
	Short: "Chariot — deploy and manage enterprise agent fleets",
	Long: `Chariot CLI.

Typical flow:
  chariot login                                  # authenticate (opens browser)
  chariot deploy --count 10000 --endpoint URL    # spin up a fleet
  chariot list                                   # see your agents + their ids

Try the round-trip without writing a backend:
  chariot demo serve                             # print replies sent to --endpoint
  chariot demo send <agent-id> "hello"           # message an agent (token-seed auth)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI, printing errors and setting a non-zero exit code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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
