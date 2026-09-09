package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chariots-sh/Chariot-CLI/internal/config"
	"github.com/chariots-sh/Chariot-CLI/internal/wallet"
)

// walletBackend fakes the wallet sign-in endpoints: it hands out a fixed
// challenge and accepts any 65-byte signature over it for that nonce (the
// real backend recovers the signer; the CLI's signing itself is pinned to
// eth_account vectors in internal/wallet).
func walletBackend(t *testing.T, onVerify func(address, nonce, sig string)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/wallet/challenge":
			var body struct{ Address string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address": wallet.ChecksumAddress(body.Address), "nonce": "n0nce",
				"message": "chariot.test wants you to sign in\nNonce: n0nce", "chain_id": 8453,
				"expires_at": "2026-09-08T00:10:00Z",
			})
		case "/v1/auth/wallet/verify", "/v1/account/wallet/link":
			var body struct{ Address, Nonce, Signature string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Nonce != "n0nce" || len(body.Signature) != 132 || !strings.HasPrefix(body.Signature, "0x") {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"detail":"bad signature"}`))
				return
			}
			onVerify(body.Address, body.Nonce, body.Signature)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "wallet-jwt", "wallet_address": strings.ToLower(body.Address)})
		default:
			w.WriteHeader(404)
		}
	}
}

// unauthenticated points the CLI at srv with an empty HOME (no session, no wallet).
func unauthenticated(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHARIOT_API_URL", srv.URL)
	t.Setenv(wallet.EnvPrivateKey, "")
	return srv
}

// `login --wallet` with no wallet yet creates one, signs the challenge with it,
// and stores the session token — no browser, no email.
func TestLoginWalletCreatesKeyAndStoresToken(t *testing.T) {
	var verified string
	unauthenticated(t, walletBackend(t, func(address, _, _ string) { verified = address }))

	got := runCLI(t, "", "login", "--wallet")
	if got.err != nil {
		t.Fatalf("login --wallet: %v\n%s", got.err, got.stdout)
	}
	mustContain(t, got.stdout, "Created a new Base wallet", "stdout")
	mustContain(t, got.stdout, "Logged in", "stdout")

	key, err := wallet.Load()
	if err != nil {
		t.Fatalf("wallet not saved: %v", err)
	}
	if verified != key.Address() {
		t.Fatalf("verified address %q, wallet is %q", verified, key.Address())
	}
	cfg, _ := config.Load()
	if cfg.Token != "wallet-jwt" {
		t.Fatalf("token not stored: %+v", cfg)
	}

	// Second login reuses the same key (no new wallet).
	again := runCLI(t, "", "login", "--wallet")
	if again.err != nil {
		t.Fatal(again.err)
	}
	mustNotContain(t, again.stdout, "Created a new", "stdout")
	mustContain(t, again.stdout, key.Address(), "stdout")
}

// `login --wallet-address` prints the message and takes the signature from stdin.
func TestLoginExternalWalletReadsSignatureFromStdin(t *testing.T) {
	unauthenticated(t, walletBackend(t, func(string, string, string) {}))
	key, _ := wallet.FromHex("0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	sig := key.SignPersonal("chariot.test wants you to sign in\nNonce: n0nce")

	got := runCLI(t, sig+"\n", "login", "--wallet-address", key.Address())
	if got.err != nil {
		t.Fatalf("login --wallet-address: %v\n%s", got.err, got.stdout)
	}
	mustContain(t, got.stdout, "-----BEGIN MESSAGE-----", "stdout")
	mustContain(t, got.stdout, "Nonce: n0nce", "stdout")
	mustContain(t, got.stdout, "Logged in", "stdout")
	if exists, _ := wallet.Exists(); exists {
		t.Fatal("external sign-in must not create a local wallet")
	}

	bad := runCLI(t, "", "login", "--wallet-address", "not-an-address")
	if bad.err == nil {
		t.Fatal("expected an address validation error")
	}
}

// `wallet link` signs with the local wallet and posts to the link endpoint.
func TestWalletLinkUsesLocalKey(t *testing.T) {
	var linked string
	login(t, walletBackend(t, func(address, _, _ string) { linked = address }))
	t.Setenv(wallet.EnvPrivateKey, "")
	if got := runCLI(t, "", "wallet", "create"); got.err != nil {
		t.Fatal(got.err)
	}
	key, _ := wallet.Load()
	got := runCLI(t, "", "wallet", "link")
	if got.err != nil {
		t.Fatalf("wallet link: %v", got.err)
	}
	if linked != key.Address() {
		t.Fatalf("linked %q, wallet is %q", linked, key.Address())
	}
	mustContain(t, got.stdout, "Linked wallet", "stdout")

	// create refuses to clobber a wallet that may hold funds.
	if again := runCLI(t, "", "wallet", "create"); again.err == nil {
		t.Fatal("wallet create must refuse to overwrite")
	}
}

func TestWalletShowsNoneWithoutKey(t *testing.T) {
	logout(t)
	t.Setenv(wallet.EnvPrivateKey, "")
	got := runCLI(t, "", "wallet")
	if got.err != nil {
		t.Fatal(got.err)
	}
	mustContain(t, got.stdout, "wallet    : none", "stdout")
}
