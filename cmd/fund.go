package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/chariots-sh/Chariot-CLI/internal/api"
	"github.com/chariots-sh/Chariot-CLI/internal/wallet"
	"github.com/spf13/cobra"
)

var (
	fundTx   string
	fundRPC  string
	fundYes  bool
	fundWait time.Duration
)

var fundCmd = &cobra.Command{
	Use:   "fund [amount]",
	Short: "Fund your account with USDC on Base (no card needed)",
	Long: `Fund your Chariot account with USDC on Base.

  chariot fund                 # show where to send USDC + your balance
  chariot fund 25              # send 25 USDC from the local wallet and credit it
  chariot fund --tx 0x…        # credit a transfer you already sent yourself

Deposits are credited by SENDER: only USDC sent from the wallet that owns
(or is linked to) your account counts — never from an exchange. With an
external wallet, send USDC to the treasury address shown by ` + "`chariot fund`" + `,
then claim the transaction hash with --tx. Sending from the local wallet
needs USDC plus a little ETH on Base for gas.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := authedClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		out := cmd.OutOrStdout()
		funding, err := client.UsdcFunding(ctx)
		if err != nil {
			return err
		}
		switch {
		case fundTx != "" && len(args) == 1:
			return errors.New("pass either an amount or --tx, not both")
		case fundTx != "":
			return claimUntilCredited(ctx, out, client, fundTx, funding)
		case len(args) == 1:
			return sendAndClaim(ctx, cmd, client, args[0], funding)
		default:
			return showFunding(ctx, out, funding)
		}
	},
}

func showFunding(ctx context.Context, out io.Writer, f *api.UsdcFunding) error {
	fmt.Fprintf(out, "network   : Base (chain %d)\n", f.ChainID)
	fmt.Fprintf(out, "token     : USDC %s\n", wallet.ChecksumAddress(f.UsdcContract))
	fmt.Fprintf(out, "send to   : %s\n", wallet.ChecksumAddress(f.TreasuryAddress))
	fmt.Fprintf(out, "send from : %s\n", senderLine(f))
	fmt.Fprintf(out, "minimum   : %s USDC, credited after %d confirmations (~%ds)\n",
		wallet.FormatUsdc(big.NewInt(f.MinDepositMicros)), f.MinConfirmations, f.MinConfirmations*2)
	if key, err := wallet.Load(); err == nil {
		fmt.Fprintf(out, "wallet    : %s (local)\n", key.Address())
		printBalances(ctx, out, key, f, fundRPC)
	}
	fmt.Fprintln(out, "\nThen: `chariot fund <amount>` to send from the local wallet, or `chariot fund --tx <hash>` after sending yourself.")
	return nil
}

func senderLine(f *api.UsdcFunding) string {
	if f.SenderAddress == "" {
		return "(no wallet on this account yet — `chariot wallet link` first)"
	}
	return wallet.ChecksumAddress(f.SenderAddress) + " only (deposits are credited by sender)"
}

func sendAndClaim(ctx context.Context, cmd *cobra.Command, client *api.Client, amountArg string, f *api.UsdcFunding) error {
	out := cmd.OutOrStdout()
	amount, err := wallet.ParseUsdcAmount(amountArg)
	if err != nil {
		return err
	}
	if amount.Cmp(big.NewInt(f.MinDepositMicros)) < 0 {
		return fmt.Errorf("minimum deposit is %s USDC", wallet.FormatUsdc(big.NewInt(f.MinDepositMicros)))
	}
	key, err := wallet.Load()
	if err != nil {
		return err
	}
	if f.SenderAddress == "" {
		return errors.New("your account has no wallet yet — run `chariot wallet link` (or sign in with `chariot login --wallet`) so the deposit can be credited")
	}
	if !strings.EqualFold(f.SenderAddress, key.Address()) {
		return fmt.Errorf("your account is linked to wallet %s, but the local wallet is %s — a transfer from it would NOT be credited",
			wallet.ChecksumAddress(f.SenderAddress), key.Address())
	}
	fmt.Fprintf(out, "Send %s USDC from %s\n  to the Chariot treasury %s on Base (chain %d)?\n",
		wallet.FormatUsdc(amount), key.Address(), wallet.ChecksumAddress(f.TreasuryAddress), f.ChainID)
	if !fundYes && !confirmFund(cmd) {
		fmt.Fprintln(out, "Cancelled.")
		return nil
	}
	rpc := wallet.NewRPC(resolveRPC(fundRPC))
	hash, err := wallet.SendUsdc(ctx, rpc, key, f.ChainID, f.UsdcContract, f.TreasuryAddress, amount)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Sent. tx %s\n", hash)
	fmt.Fprintf(out, "(if this command is interrupted, credit it later with `chariot fund --tx %s`)\n", hash)
	return claimUntilCredited(ctx, out, client, hash, f)
}

func confirmFund(cmd *cobra.Command) bool {
	fmt.Fprint(cmd.OutOrStdout(), "Proceed? [y/N] ")
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// claimUntilCredited claims the tx, re-trying while the backend reports it
// as pending (mined but under the confirmation depth) or the RPC hasn't seen
// it yet, until fundWait elapses.
func claimUntilCredited(ctx context.Context, out io.Writer, client *api.Client, txHash string, f *api.UsdcFunding) error {
	deadline := time.Now().Add(fundWait)
	interval := 3 * time.Second
	waiting := false
	for {
		claim, err := client.ClaimUsdc(ctx, txHash)
		if err != nil {
			var apiErr *api.APIError
			// "not found" is what an unmined tx looks like to the backend; keep
			// waiting for it rather than failing the moment after a send.
			if errors.As(err, &apiErr) && apiErr.Status == 400 && strings.Contains(apiErr.Detail, "not found") && time.Now().Before(deadline) {
				if !waiting {
					fmt.Fprint(out, "Waiting for the transaction to be mined")
					waiting = true
				}
			} else {
				return err
			}
		} else if !claim.Pending() {
			if waiting {
				fmt.Fprintln(out)
			}
			return reportClaim(out, claim, f)
		} else {
			if !waiting {
				fmt.Fprintf(out, "Mined; waiting for %d confirmations", f.MinConfirmations)
				waiting = true
			}
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(out)
			return fmt.Errorf("still not confirmed after %s — run `chariot fund --tx %s` again in a minute", fundWait, txHash)
		}
		fmt.Fprint(out, ".")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func reportClaim(out io.Writer, c *api.UsdcClaim, f *api.UsdcFunding) error {
	amount := wallet.FormatUsdc(big.NewInt(c.AmountMicros))
	switch c.Status {
	case "credited":
		fmt.Fprintf(out, "✓ Credited %s USDC (tx %s).\n", amount, c.TxHash)
	case "already_credited":
		fmt.Fprintf(out, "Already credited: %s USDC (tx %s).\n", amount, c.TxHash)
	default:
		fmt.Fprintf(out, "%s: %s USDC (tx %s)\n", c.Status, amount, c.TxHash)
	}
	if c.BalanceAfterMicros != nil {
		fmt.Fprintf(out, "credits   : $%s\n", wallet.FormatUsdc(big.NewInt(*c.BalanceAfterMicros)))
	}
	return nil
}

func init() {
	fundCmd.Flags().StringVar(&fundTx, "tx", "", "credit a USDC transfer you already sent (transaction hash)")
	fundCmd.Flags().StringVar(&fundRPC, "rpc", "", "Base JSON-RPC endpoint for sending (default $"+wallet.EnvRPC+" or "+wallet.DefaultBaseRPC+")")
	fundCmd.Flags().BoolVarP(&fundYes, "yes", "y", false, "send without the confirmation prompt")
	fundCmd.Flags().DurationVar(&fundWait, "wait", 3*time.Minute, "how long to wait for the deposit to confirm")
	rootCmd.AddCommand(fundCmd)
}
