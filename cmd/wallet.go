package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/chariots-sh/Chariot-CLI/internal/wallet"
	"github.com/spf13/cobra"
)

var walletRPC string

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Show the CLI's Base wallet (address, USDC/ETH balance, link status)",
	Long: `Show the local Base wallet the CLI signs in and pays with.

The wallet is a plain secp256k1 key in ~/.chariot/wallet.json (created by
` + "`chariot login --wallet`" + ` or ` + "`chariot wallet create`" + `). To fund your
Chariot account, send USDC on Base to this address, then run ` + "`chariot fund <amount>`" + `
— it also needs a little ETH on Base for gas.

Subcommands:
  chariot wallet create     # generate a new wallet (refuses to overwrite one)
  chariot wallet import     # import an existing private key (read from stdin)
  chariot wallet link       # bind this wallet to your signed-in (email) account`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		ctx := cmd.Context()
		key, err := wallet.Load()
		if errors.Is(err, wallet.ErrNoWallet) {
			fmt.Fprintln(out, "wallet    : none")
			fmt.Fprintln(out, "\nCreate one with `chariot wallet create` (or `chariot login --wallet`).")
			return nil
		}
		if err != nil {
			return err
		}
		source := "~/.chariot/wallet.json"
		if os.Getenv(wallet.EnvPrivateKey) != "" {
			source = wallet.EnvPrivateKey
		}
		fmt.Fprintf(out, "wallet    : %s\n", key.Address())
		fmt.Fprintf(out, "key       : %s\n", source)

		funding := defaultFunding()
		if client, _, err := authedClient(); err == nil {
			if acct, err := client.Account(ctx); err == nil {
				fmt.Fprintf(out, "account   : %s\n", linkStatus(acct, key.Address()))
			}
			if f, err := client.UsdcFunding(ctx); err == nil {
				funding = f
			}
		}
		printBalances(ctx, out, key, funding, walletRPC)
		return nil
	},
}

func linkStatus(acct *api.Account, address string) string {
	switch {
	case strings.EqualFold(acct.WalletAddress, address):
		return "linked (this wallet's USDC deposits are credited to it)"
	case acct.WalletAddress != "":
		return fmt.Sprintf("linked to a DIFFERENT wallet %s — deposits from this one won't be credited", acct.WalletAddress)
	default:
		return "not linked — run `chariot wallet link` so USDC from this wallet is credited"
	}
}

// defaultFunding is what to show when not logged in: Base mainnet USDC. The
// treasury is unknown without an account, so it stays empty.
func defaultFunding() *api.UsdcFunding {
	return &api.UsdcFunding{ChainID: 8453, UsdcContract: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"}
}

func resolveRPC(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv(wallet.EnvRPC); v != "" {
		return v
	}
	return wallet.DefaultBaseRPC
}

func printBalances(ctx context.Context, out interface{ Write([]byte) (int, error) }, key *wallet.Key, funding *api.UsdcFunding, rpcFlag string) {
	rpc := wallet.NewRPC(resolveRPC(rpcFlag))
	usdc, err := rpc.UsdcBalance(ctx, funding.UsdcContract, key.AddressBytes())
	if err != nil {
		fmt.Fprintf(out, "balance   : (unavailable: %v)\n", err)
		return
	}
	eth, err := rpc.EthBalance(ctx, key.Address())
	if err != nil {
		fmt.Fprintf(out, "balance   : %s USDC (ETH unavailable: %v)\n", wallet.FormatUsdc(usdc), err)
		return
	}
	fmt.Fprintf(out, "balance   : %s USDC, %s ETH (chain %d)\n", wallet.FormatUsdc(usdc), wallet.FormatEth(eth), funding.ChainID)
}

var walletCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Generate a new local Base wallet",
	RunE: func(cmd *cobra.Command, args []string) error {
		if ok, err := wallet.Exists(); err != nil {
			return err
		} else if ok {
			p, _ := wallet.Path()
			return fmt.Errorf("%s already exists — it may hold funds, so it is never overwritten", p)
		}
		key, err := wallet.Generate()
		if err != nil {
			return err
		}
		if err := wallet.Save(key); err != nil {
			return err
		}
		p, _ := wallet.Path()
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Created wallet %s\n", key.Address())
		fmt.Fprintf(out, "key: %s (0600 — back it up; it will hold your funds)\n", p)
		fmt.Fprintln(out, "\nNext: `chariot login --wallet`, send USDC (and a little ETH for gas) on Base to the address, then `chariot fund <amount>`.")
		return nil
	},
}

var walletImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import an existing private key (read from stdin, never from argv)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if ok, err := wallet.Exists(); err != nil {
			return err
		} else if ok {
			p, _ := wallet.Path()
			return fmt.Errorf("%s already exists — move it aside before importing another key", p)
		}
		out := cmd.OutOrStdout()
		fmt.Fprint(out, "Private key (0x… hex): ")
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("reading key: %w", err)
		}
		key, err := wallet.FromHex(strings.TrimSpace(line))
		if err != nil {
			return err
		}
		if err := wallet.Save(key); err != nil {
			return err
		}
		fmt.Fprintf(out, "Imported wallet %s\n", key.Address())
		return nil
	},
}

var walletLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Bind the local wallet to your signed-in account (so its USDC deposits are credited)",
	Long: `Bind the local wallet to the account you're signed in to.

Deposits are credited by SENDER address, so an account created by email
needs a wallet linked before ` + "`chariot fund`" + ` can credit anything. An
account created with ` + "`chariot login --wallet`" + ` is already linked.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		key, err := loadOrCreateWallet(cmd.OutOrStdout())
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		challenge, err := client.StartWalletChallenge(ctx, key.Address())
		if err != nil {
			return fmt.Errorf("starting wallet challenge: %w", err)
		}
		linked, err := client.LinkWallet(ctx, key.Address(), challenge.Nonce, key.SignPersonal(challenge.Message))
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Linked wallet %s to your account.\n", wallet.ChecksumAddress(linked))
		return nil
	},
}

func init() {
	walletCmd.Flags().StringVar(&walletRPC, "rpc", "", "Base JSON-RPC endpoint for balances (default $"+wallet.EnvRPC+" or "+wallet.DefaultBaseRPC+")")
	walletCmd.AddCommand(walletCreateCmd, walletImportCmd, walletLinkCmd)
	rootCmd.AddCommand(walletCmd)
}
