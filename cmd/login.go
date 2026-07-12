package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate the CLI (opens your browser)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		client := api.New(cfg.BaseURL(), "")
		ctx := cmd.Context()

		start, err := client.StartDeviceAuth(ctx)
		if err != nil {
			return fmt.Errorf("starting login: %w", err)
		}

		fmt.Printf("\nTo sign in, visit:\n\n  %s\n\n", start.VerificationURIComplete)
		fmt.Printf("and confirm this code:  %s\n\n", start.UserCode)
		if err := openBrowser(start.VerificationURIComplete); err == nil {
			fmt.Println("(opened your browser…)")
		}
		fmt.Println("Waiting for approval…")

		token, err := pollForToken(ctx, client, start)
		if err != nil {
			return err
		}

		cfg.Token = token
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("\n✓ Logged in.")
		return nil
	},
}

func pollForToken(ctx context.Context, client *api.Client, start *api.DeviceStart) (string, error) {
	interval := time.Duration(max(start.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(start.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		token, err := client.PollDeviceAuth(ctx, start.DeviceCode)
		if err != nil {
			return "", err
		}
		if token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("login timed out — run `chariot login` again")
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
