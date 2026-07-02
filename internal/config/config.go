// Package config loads and persists the CLI's local state (~/.chariot/config.json):
// the API base URL and the session token obtained from `chariot login`.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultAPIURL is the hosted Chariot backend; override with CHARIOT_API_URL or
// by setting api_url in the config file (handy for local dev).
const DefaultAPIURL = "https://app.chariots.sh"

// Config is the on-disk CLI state.
type Config struct {
	APIURL string `json:"api_url,omitempty"`
	Token  string `json:"token,omitempty"`
}

// Path returns ~/.chariot/config.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".chariot", "config.json"), nil
}

// Load reads the config, returning an empty (non-nil) Config if none exists yet.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	return cfg, nil
}

// Save writes the config with 0600 perms (it holds a session token).
func Save(cfg *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// BaseURL resolves the API base URL: CHARIOT_API_URL env > config > default.
func (c *Config) BaseURL() string {
	if v := os.Getenv("CHARIOT_API_URL"); v != "" {
		return v
	}
	if c.APIURL != "" {
		return c.APIURL
	}
	return DefaultAPIURL
}
