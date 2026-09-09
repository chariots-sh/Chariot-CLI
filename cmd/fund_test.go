package cmd

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chariots-sh/Chariot-CLI/internal/wallet"
)

const (
	testTreasury = "0xabababababababababababababababababababab"
	testUsdc     = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	testKey      = "0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	testSender   = "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23"
)

// fundingBackend serves the funding info and a claim endpoint that reports
// `pending` the first pendingClaims times, then `credited`.
func fundingBackend(t *testing.T, sender string, pendingClaims int, claimed *[]string) http.HandlerFunc {
	t.Helper()
	var mu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/funding/usdc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chain_id": 8453, "usdc_contract": testUsdc, "treasury_address": testTreasury,
				"sender_address": sender, "min_deposit_micros": 1_000_000, "min_confirmations": 15,
			})
		case "/v1/account/funding/usdc/claim":
			var body struct {
				TxHash string `json:"tx_hash"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			*claimed = append(*claimed, body.TxHash)
			n := len(*claimed)
			mu.Unlock()
			if n <= pendingClaims {
				w.WriteHeader(202)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "pending", "tx_hash": body.TxHash, "amount_micros": 0, "confirmations": n, "balance_after_micros": nil,
				})
				return
			}
			balance := int64(25_000_000)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "credited", "tx_hash": body.TxHash, "amount_micros": 25_000_000, "confirmations": 15, "balance_after_micros": balance,
			})
		default:
			w.WriteHeader(404)
		}
	}
}

func TestFundShowsInstructions(t *testing.T) {
	var claimed []string
	login(t, fundingBackend(t, "", 0, &claimed))
	t.Setenv(wallet.EnvPrivateKey, "")

	got := runCLI(t, "", "fund")
	if got.err != nil {
		t.Fatalf("fund: %v", got.err)
	}
	mustContain(t, got.stdout, "Base (chain 8453)", "stdout")
	mustContain(t, got.stdout, wallet.ChecksumAddress(testTreasury), "treasury")
	mustContain(t, got.stdout, "no wallet on this account yet", "sender hint")
	mustContain(t, got.stdout, "minimum   : 1.00 USDC", "minimum")
}

// --tx claims an already-sent transfer, polling through `pending`.
func TestFundTxPollsUntilCredited(t *testing.T) {
	var claimed []string
	login(t, fundingBackend(t, testSender, 1, &claimed))
	tx := "0x" + strings.Repeat("ab", 32)

	got := runCLI(t, "", "fund", "--tx", tx, "--wait", "10s")
	if got.err != nil {
		t.Fatalf("fund --tx: %v\n%s", got.err, got.stdout)
	}
	if len(claimed) != 2 || claimed[0] != tx {
		t.Fatalf("expected two claims of %s, got %v", tx, claimed)
	}
	mustContain(t, got.stdout, "waiting for 15 confirmations", "pending notice")
	mustContain(t, got.stdout, "Credited 25.00 USDC", "stdout")
	mustContain(t, got.stdout, "credits   : $25.00", "balance")
}

// fakeRPC answers just the JSON-RPC methods SendUsdc uses, recording the
// raw transaction it is asked to broadcast.
func fakeRPC(t *testing.T, usdcBalance, ethBalance *big.Int, sent *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x2105"
		case "eth_call":
			result = "0x" + usdcBalance.Text(16)
		case "eth_getBalance":
			result = "0x" + ethBalance.Text(16)
		case "eth_estimateGas":
			result = "0xc350" // 50_000
		case "eth_maxPriorityFeePerGas":
			result = "0xf4240"
		case "eth_getBlockByNumber":
			result = map[string]string{"baseFeePerGas": "0x5f5e100"}
		case "eth_getTransactionCount":
			result = "0x7"
		case "eth_sendRawTransaction":
			*sent = req.Params[0].(string)
			result = "0x" + strings.Repeat("cd", 32)
		default:
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// `fund 25` signs an EIP-1559 USDC transfer with the local key, broadcasts it
// through the RPC, then claims the returned hash until credited.
func TestFundAmountSendsFromLocalWalletAndClaims(t *testing.T) {
	var claimed []string
	login(t, fundingBackend(t, testSender, 0, &claimed))
	t.Setenv(wallet.EnvPrivateKey, testKey)
	var sent string
	rpc := fakeRPC(t, big.NewInt(100_000_000), big.NewInt(1e16), &sent)

	got := runCLI(t, "", "fund", "25", "--yes", "--rpc", rpc.URL)
	if got.err != nil {
		t.Fatalf("fund 25: %v\n%s", got.err, got.stdout)
	}
	if !strings.HasPrefix(sent, "0x02") {
		t.Fatalf("expected a type-2 raw tx to be broadcast, got %q", sent)
	}
	// The calldata is transfer(treasury, 25 USDC).
	mustContain(t, sent, "a9059cbb000000000000000000000000"+strings.TrimPrefix(testTreasury, "0x"), "calldata")
	mustContain(t, sent, "17d7840", "amount 25_000_000")
	if len(claimed) != 1 || claimed[0] != "0x"+strings.Repeat("cd", 32) {
		t.Fatalf("claim of the broadcast hash expected, got %v", claimed)
	}
	mustContain(t, got.stdout, "Credited 25.00 USDC", "stdout")

	// The prompt cancels on anything but y/yes, and nothing is sent.
	sent = ""
	cancelled := runCLI(t, "n\n", "fund", "25", "--rpc", rpc.URL)
	if cancelled.err != nil || sent != "" {
		t.Fatalf("cancel: err=%v sent=%q", cancelled.err, sent)
	}
	mustContain(t, cancelled.stdout, "Cancelled", "stdout")
}

func TestFundRefusesWhenLocalWalletIsNotTheAccountWallet(t *testing.T) {
	var claimed []string
	login(t, fundingBackend(t, "0x"+strings.Repeat("ef", 20), 0, &claimed))
	t.Setenv(wallet.EnvPrivateKey, testKey)
	var sent string
	rpc := fakeRPC(t, big.NewInt(100_000_000), big.NewInt(1e16), &sent)

	got := runCLI(t, "", "fund", "25", "--yes", "--rpc", rpc.URL)
	if got.err == nil || !strings.Contains(got.err.Error(), "would NOT be credited") {
		t.Fatalf("expected a sender-mismatch refusal, got %v", got.err)
	}
	if sent != "" {
		t.Fatal("nothing must be broadcast when the sender would not be credited")
	}
}

func TestFundRefusesInsufficientUsdc(t *testing.T) {
	var claimed []string
	login(t, fundingBackend(t, testSender, 0, &claimed))
	t.Setenv(wallet.EnvPrivateKey, testKey)
	var sent string
	rpc := fakeRPC(t, big.NewInt(5_000_000), big.NewInt(1e16), &sent)

	got := runCLI(t, "", "fund", "25", "--yes", "--rpc", rpc.URL)
	if got.err == nil || !strings.Contains(got.err.Error(), "holds 5.00 USDC") {
		t.Fatalf("expected an insufficient-balance error, got %v", got.err)
	}
	if sent != "" {
		t.Fatal("nothing must be broadcast without balance")
	}
}
