// Package wallet is the CLI's Base (EVM) wallet: a locally generated secp256k1
// key (~/.chariot/wallet.json) that signs Chariot's sign-in challenge and,
// when funded, sends USDC to the Chariot treasury. No email, no card, no
// browser — the key IS the account.
//
// Only what the CLI needs is implemented (EIP-191 personal_sign, EIP-1559
// transfers, a handful of JSON-RPC calls); it is not a general Ethereum
// library.
package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Key is a secp256k1 private key with its Ethereum address.
type Key struct {
	priv *secp256k1.PrivateKey
}

// Generate makes a fresh random key.
func Generate() (*Key, error) {
	priv, err := secp256k1.GeneratePrivateKeyFromRand(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}
	return &Key{priv: priv}, nil
}

// FromHex parses a 32-byte private key given as hex (with or without 0x).
func FromHex(s string) (*Key, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
	if err != nil {
		return nil, fmt.Errorf("private key is not hex: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(raw))
	}
	priv := secp256k1.PrivKeyFromBytes(raw)
	if priv.Key.IsZero() {
		return nil, errors.New("private key must not be zero")
	}
	return &Key{priv: priv}, nil
}

// Hex returns the private key as 0x-hex. Handle with care.
func (k *Key) Hex() string {
	return "0x" + hex.EncodeToString(k.priv.Serialize())
}

// Address returns the EIP-55 checksummed address.
func (k *Key) Address() string {
	pub := k.priv.PubKey().SerializeUncompressed() // 0x04 ‖ X ‖ Y
	h := Keccak256(pub[1:])
	return ChecksumAddress("0x" + hex.EncodeToString(h[12:]))
}

// AddressBytes is the 20-byte address.
func (k *Key) AddressBytes() [20]byte {
	var out [20]byte
	raw, _ := hex.DecodeString(strings.TrimPrefix(k.Address(), "0x"))
	copy(out[:], raw)
	return out
}

// SignPersonal signs a text message under EIP-191 personal_sign (the
// "\x19Ethereum Signed Message:\n<len>" prefix every wallet app applies),
// returning the 65-byte r‖s‖v signature as 0x-hex with v ∈ {27, 28}.
func (k *Key) SignPersonal(message string) string {
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	return "0x" + hex.EncodeToString(k.signRecoverable(Keccak256([]byte(prefixed)), 27))
}

// signRecoverable produces r‖s‖(vBase+recoveryID). decred's compact format
// puts the recovery byte FIRST (27 + id, +4 when compressed); Ethereum wants
// it last, so the bytes are re-ordered here.
func (k *Key) signRecoverable(hash []byte, vBase byte) []byte {
	compact := ecdsa.SignCompact(k.priv, hash, false)
	sig := make([]byte, 65)
	copy(sig[:64], compact[1:65])
	sig[64] = compact[0] - 27 + vBase
	return sig
}

// Keccak256 is the Ethereum hash (NOT the NIST SHA3-256; different padding).
func Keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// ChecksumAddress renders a hex address in EIP-55 mixed case.
func ChecksumAddress(address string) string {
	lower := strings.ToLower(strings.TrimPrefix(address, "0x"))
	hash := hex.EncodeToString(Keccak256([]byte(lower)))
	var b strings.Builder
	b.WriteString("0x")
	for i, c := range lower {
		if c >= 'a' && c <= 'f' && hash[i] >= '8' {
			b.WriteByte(byte(c) - 'a' + 'A')
			continue
		}
		b.WriteByte(byte(c))
	}
	return b.String()
}

// IsAddress reports whether s is 0x + 40 hex chars.
func IsAddress(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

// --- on-disk store ---------------------------------------------------------

// EnvPrivateKey names the env var that supplies a key without touching disk
// (CI / headless agents). When set it wins over the wallet file.
const EnvPrivateKey = "CHARIOT_WALLET_PRIVATE_KEY"

type fileFormat struct {
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

// Path returns ~/.chariot/wallet.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".chariot", "wallet.json"), nil
}

// ErrNoWallet is returned by Load when neither the env var nor the file exists.
var ErrNoWallet = errors.New("no wallet: run `chariot wallet create` (or set " + EnvPrivateKey + ")")

// Load returns the key from CHARIOT_WALLET_PRIVATE_KEY, else the wallet file.
func Load() (*Key, error) {
	if v := os.Getenv(EnvPrivateKey); v != "" {
		return FromHex(v)
	}
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNoWallet
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	return FromHex(f.PrivateKey)
}

// Exists reports whether a wallet file is on disk (ignores the env var).
func Exists() (bool, error) {
	p, err := Path()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Save writes the key with 0600 perms. Refuses to overwrite an existing file —
// a wallet holds funds, so replacing it is an explicit `chariot wallet import`.
func Save(k *Key) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileFormat{PrivateKey: k.Hex(), Address: k.Address()}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists — a wallet holds funds, so it is never overwritten; move it aside first", p)
		}
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
