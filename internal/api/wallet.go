package api

import (
	"context"
	"net/http"
)

// --- wallet sign-in --------------------------------------------------------

// WalletChallenge is the one-time Sign-In-with-Ethereum challenge the wallet
// signs. Message must be signed byte-for-byte (EIP-191 personal_sign).
type WalletChallenge struct {
	Address   string `json:"address"` // EIP-55 checksummed, as in the message
	Nonce     string `json:"nonce"`
	Message   string `json:"message"`
	ChainID   int64  `json:"chain_id"`
	ExpiresAt string `json:"expires_at"`
}

// StartWalletChallenge mints a sign-in challenge for a Base wallet address.
func (c *Client) StartWalletChallenge(ctx context.Context, address string) (*WalletChallenge, error) {
	out := &WalletChallenge{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/auth/wallet/challenge",
		map[string]string{"address": address}, out); err != nil {
		return nil, err
	}
	return out, nil
}

type walletProof struct {
	Address   string `json:"address"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

// VerifyWalletChallenge exchanges a signed challenge for a session token,
// creating the wallet-owned account on first sign-in.
func (c *Client) VerifyWalletChallenge(ctx context.Context, address, nonce, signature string) (string, error) {
	out := struct {
		Token string `json:"token"`
	}{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/auth/wallet/verify",
		walletProof{Address: address, Nonce: nonce, Signature: signature}, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// LinkWallet binds a wallet to the signed-in (email) account after it signs
// a challenge, so USDC it sends to the treasury is credited here.
func (c *Client) LinkWallet(ctx context.Context, address, nonce, signature string) (string, error) {
	out := struct {
		WalletAddress string `json:"wallet_address"`
	}{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/account/wallet/link",
		walletProof{Address: address, Nonce: nonce, Signature: signature}, &out); err != nil {
		return "", err
	}
	return out.WalletAddress, nil
}

// --- USDC funding ----------------------------------------------------------

// UsdcFunding is where and how to send USDC on Base to fund the account.
type UsdcFunding struct {
	ChainID         int64  `json:"chain_id"`
	UsdcContract    string `json:"usdc_contract"`
	TreasuryAddress string `json:"treasury_address"`
	// The account's wallet — the ONLY sender whose transfers are credited.
	// "" until a wallet has signed in / been linked.
	SenderAddress    string `json:"sender_address"`
	MinDepositMicros int64  `json:"min_deposit_micros"`
	MinConfirmations int64  `json:"min_confirmations"`
}

// UsdcFunding fetches the funding instructions for the account.
func (c *Client) UsdcFunding(ctx context.Context) (*UsdcFunding, error) {
	out := &UsdcFunding{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/account/funding/usdc", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UsdcClaim is the outcome of claiming a deposit transaction.
type UsdcClaim struct {
	Status             string `json:"status"` // credited | already_credited | pending
	TxHash             string `json:"tx_hash"`
	AmountMicros       int64  `json:"amount_micros"`
	Confirmations      int64  `json:"confirmations"`
	BalanceAfterMicros *int64 `json:"balance_after_micros"`
}

// Pending reports whether the deposit is mined but not yet confirmed deep
// enough to credit — claim again later.
func (u *UsdcClaim) Pending() bool { return u.Status == "pending" }

// ClaimUsdc credits a USDC transfer the account's wallet made to the
// treasury. Idempotent: a credited hash reports already_credited.
func (c *Client) ClaimUsdc(ctx context.Context, txHash string) (*UsdcClaim, error) {
	out := &UsdcClaim{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/account/funding/usdc/claim",
		map[string]string{"tx_hash": txHash}, out); err != nil {
		return nil, err
	}
	return out, nil
}
