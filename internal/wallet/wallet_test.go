package wallet

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

// Vectors produced with eth_account (Python) for this well-known test key.
const (
	vectorKey     = "0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	vectorAddress = "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23"
	vectorMessage = "app.chariots.sh wants you to sign in with your Ethereum account:\n" +
		vectorAddress + "\n\nSign in to Chariot.\n\nURI: https://app.chariots.sh\nVersion: 1\n" +
		"Chain ID: 8453\nNonce: abc123\nIssued At: 2026-09-08T00:00:00Z\nExpiration Time: 2026-09-08T00:10:00Z"
	vectorSig  = "0x8780a3eb334f6a2e4a0d4bbd8e40db2f35c556c6b9875098683ed7065e7ab97b264ecf93c43ec0b7b08fefeb5c0d75e14b92413500d3ed2e8fe552a0af87dda41b"
	vectorRaw  = "0x02f8b082210507830f42408402faf08082ea6094833589fcd6edb6e08f4c7c32d4f71b54bda0291380b844a9059cbb000000000000000000000000abababababababababababababababababababab00000000000000000000000000000000000000000000000000000000017d7840c001a0cfd5dcc7e04e247e80837b5c9b564b0d1b9499293a32eece03f08a23038a06aca01b8a58345d5e82a71d7e1efba0cfedbe9963ddb729bd557d6ee8c85d24183c5c"
	vectorHash = "0xcac9bb9b948fc2a49c3e654c6c3bff057da6c8089aa86d307ea68d513a66e553"
)

func TestAddressAndPersonalSignMatchEthAccount(t *testing.T) {
	k, err := FromHex(vectorKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Address(); got != vectorAddress {
		t.Fatalf("address: got %s want %s", got, vectorAddress)
	}
	if got := k.SignPersonal(vectorMessage); got != vectorSig {
		t.Fatalf("personal_sign:\n got %s\nwant %s", got, vectorSig)
	}
}

func TestEip1559UsdcTransferMatchesEthAccount(t *testing.T) {
	k, _ := FromHex(vectorKey)
	to, _ := ParseAddress("0xabababababababababababababababababababab")
	token, _ := ParseAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
	tx := &Eip1559Tx{
		ChainID:              big.NewInt(8453),
		Nonce:                7,
		MaxPriorityFeePerGas: big.NewInt(1_000_000),
		MaxFeePerGas:         big.NewInt(50_000_000),
		Gas:                  60_000,
		To:                   token,
		Value:                big.NewInt(0),
		Data:                 TransferCalldata(to, big.NewInt(25_000_000)),
	}
	raw, hash := tx.Sign(k)
	if raw != vectorRaw {
		t.Fatalf("raw tx:\n got %s\nwant %s", raw, vectorRaw)
	}
	if hash != vectorHash {
		t.Fatalf("hash: got %s want %s", hash, vectorHash)
	}
}

func TestChecksumAddress(t *testing.T) {
	if got := ChecksumAddress("0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"); got != "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913" {
		t.Fatalf("got %s", got)
	}
}

func TestParseAndFormatUsdc(t *testing.T) {
	cases := map[string]int64{"25": 25_000_000, "$12.50": 12_500_000, "0.000001": 1, ".5": 500_000}
	for in, want := range cases {
		got, err := ParseUsdcAmount(in)
		if err != nil || got.Int64() != want {
			t.Errorf("%q: got %v, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-1", "1.2345678", "abc", ""} {
		if _, err := ParseUsdcAmount(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	if got := FormatUsdc(big.NewInt(12_500_000)); got != "12.50" {
		t.Errorf("format: %s", got)
	}
	if got := FormatUsdc(big.NewInt(1)); got != "0.000001" {
		t.Errorf("format sub-cent: %s", got)
	}
}

func TestSaveLoadRoundTripAndNoOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvPrivateKey, "")
	if _, err := Load(); err != ErrNoWallet {
		t.Fatalf("expected ErrNoWallet, got %v", err)
	}
	k, _ := Generate()
	if err := Save(k); err != nil {
		t.Fatal(err)
	}
	p, _ := Path()
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Fatalf("want 0600, got %v", info.Mode().Perm())
	}
	if filepath.Base(p) != "wallet.json" {
		t.Fatalf("unexpected path %s", p)
	}
	got, err := Load()
	if err != nil || got.Address() != k.Address() {
		t.Fatalf("round-trip: %v %v", got, err)
	}
	other, _ := Generate()
	if err := Save(other); err == nil {
		t.Fatal("overwrite must be refused")
	}
	// The env var wins over the file.
	t.Setenv(EnvPrivateKey, vectorKey)
	fromEnv, err := Load()
	if err != nil || fromEnv.Address() != vectorAddress {
		t.Fatalf("env key: %v %v", fromEnv, err)
	}
}

// Base gas budgets are sub-microether; the refusal message must not round
// them to "0 ETH".
func TestFormatEthKeepsTinyAmountsVisible(t *testing.T) {
	cases := map[int64]string{0: "0", 420_000_000_000: "0.00000042", 1_500_000_000_000_000: "0.0015", 1e18: "1"}
	for wei, want := range cases {
		if got := FormatEth(big.NewInt(wei)); got != want {
			t.Errorf("%d wei: got %s want %s", wei, got, want)
		}
	}
}
