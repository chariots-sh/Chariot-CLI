package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/chariots-sh/Chariot-CLI/internal/wallet"
	"github.com/spf13/cobra"
)

var (
	loginWallet        bool
	loginWalletAddress string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate the CLI (browser, or a Base wallet — no email needed)",
	Long: `Authenticate the CLI.

Default: opens your browser to sign in with an email code and approve the CLI.

  chariot login

Wallet: no email, no card, no browser. The CLI signs a one-time challenge
with a Base wallet key; the first sign-in creates the account. Fund it by
sending USDC on Base (see ` + "`chariot fund`" + `).

  chariot login --wallet                  # use (or create) ~/.chariot/wallet.json
  chariot login --wallet-address 0x…      # sign with your own wallet app instead

With --wallet-address the CLI prints the message to sign; sign it in your
wallet (MetaMask, Rabby, Coinbase Wallet, ` + "`cast wallet sign`" + `…) and paste
the signature back. Set ` + wallet.EnvPrivateKey + ` to sign headlessly
with a key that never touches disk.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		client := api.New(cfg.BaseURL(), "")
		ctx := cmd.Context()
		out := cmd.OutOrStdout()

		var token string
		switch {
		case loginWallet && loginWalletAddress != "":
			return errors.New("use either --wallet or --wallet-address, not both")
		case loginWallet:
			token, err = loginWithLocalWallet(ctx, cmd, client)
		case loginWalletAddress != "":
			token, err = loginWithExternalWallet(ctx, cmd, client, loginWalletAddress)
		default:
			token, err = loginWithBrowser(ctx, out, client)
		}
		if err != nil {
			return err
		}

		cfg.Token = token
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintln(out, "\n✓ Logged in.")
		return nil
	},
}

func loginWithBrowser(ctx context.Context, out io.Writer, client *api.Client) (string, error) {
	start, err := client.StartDeviceAuth(ctx)
	if err != nil {
		return "", fmt.Errorf("starting login: %w", err)
	}

	fmt.Fprintf(out, "\nTo sign in, visit:\n\n  %s\n\n", start.VerificationURIComplete)
	fmt.Fprintf(out, "and confirm this code:  %s\n\n", start.UserCode)
	if err := openBrowser(start.VerificationURIComplete); err == nil {
		fmt.Fprintln(out, "(opened your browser…)")
	}
	fmt.Fprintln(out, "Waiting for approval…")
	return pollForToken(ctx, client, start)
}

// loginWithLocalWallet signs the challenge with the CLI's own key, creating
// the key on first use — the fully headless path (no browser, no email).
func loginWithLocalWallet(ctx context.Context, cmd *cobra.Command, client *api.Client) (string, error) {
	out := cmd.OutOrStdout()
	key, err := loadOrCreateWallet(out)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Signing in as wallet %s…\n", key.Address())
	return walletSignIn(ctx, client, key.Address(), func(message string) (string, error) {
		return key.SignPersonal(message), nil
	})
}

// loginWithExternalWallet asks the user's own wallet app for the signature.
func loginWithExternalWallet(ctx context.Context, cmd *cobra.Command, client *api.Client, address string) (string, error) {
	if !wallet.IsAddress(address) {
		return "", fmt.Errorf("--wallet-address must be a 0x… Base address, got %q", address)
	}
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())
	return walletSignIn(ctx, client, address, func(message string) (string, error) {
		fmt.Fprintf(out, "\nSign this message with %s (personal_sign — it costs no gas):\n\n", wallet.ChecksumAddress(address))
		fmt.Fprintln(out, "-----BEGIN MESSAGE-----")
		fmt.Fprintln(out, message)
		fmt.Fprintln(out, "-----END MESSAGE-----")
		fmt.Fprint(out, "\nPaste the signature (0x…): ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("reading signature: %w", err)
		}
		sig := strings.TrimSpace(line)
		if sig == "" {
			return "", errors.New("no signature entered")
		}
		return sig, nil
	})
}

// walletSignIn runs challenge → sign → verify and returns the session token.
func walletSignIn(ctx context.Context, client *api.Client, address string, sign func(message string) (string, error)) (string, error) {
	challenge, err := client.StartWalletChallenge(ctx, address)
	if err != nil {
		return "", fmt.Errorf("starting wallet sign-in: %w", err)
	}
	signature, err := sign(challenge.Message)
	if err != nil {
		return "", err
	}
	token, err := client.VerifyWalletChallenge(ctx, address, challenge.Nonce, signature)
	if err != nil {
		return "", fmt.Errorf("verifying wallet signature: %w", err)
	}
	return token, nil
}

// loadOrCreateWallet returns the local wallet key, generating and saving a
// fresh one if none exists yet (and saying so — the user needs to know
// where their funds' key lives).
func loadOrCreateWallet(out io.Writer) (*wallet.Key, error) {
	key, err := wallet.Load()
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, wallet.ErrNoWallet) {
		return nil, err
	}
	key, err = wallet.Generate()
	if err != nil {
		return nil, err
	}
	if err := wallet.Save(key); err != nil {
		return nil, err
	}
	p, _ := wallet.Path()
	fmt.Fprintf(out, "Created a new Base wallet %s\n  key: %s (0600 — back it up; it will hold your funds)\n", key.Address(), p)
	return key, nil
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
	loginCmd.Flags().BoolVar(&loginWallet, "wallet", false, "sign in with the local Base wallet (~/.chariot/wallet.json), creating it if needed")
	loginCmd.Flags().StringVar(&loginWalletAddress, "wallet-address", "", "sign in with an external wallet: prints the message to sign, reads the signature")
	rootCmd.AddCommand(loginCmd)
}
