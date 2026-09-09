package wallet

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// USDC has 6 decimals: one token base unit is one micro-dollar, which is
// also Chariot's ledger unit — no conversion anywhere in between.
const UsdcDecimals = 6

var (
	selectorTransfer  = []byte{0xa9, 0x05, 0x9c, 0xbb} // transfer(address,uint256)
	selectorBalanceOf = []byte{0x70, 0xa0, 0x82, 0x31} // balanceOf(address)
)

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// TransferCalldata encodes ERC-20 transfer(to, amount).
func TransferCalldata(to [20]byte, amount *big.Int) []byte {
	data := append([]byte{}, selectorTransfer...)
	data = append(data, pad32(to[:])...)
	return append(data, pad32(amount.Bytes())...)
}

// BalanceOfCalldata encodes ERC-20 balanceOf(owner).
func BalanceOfCalldata(owner [20]byte) []byte {
	return append(append([]byte{}, selectorBalanceOf...), pad32(owner[:])...)
}

// ParseAddress decodes a 0x-hex address into 20 bytes.
func ParseAddress(s string) ([20]byte, error) {
	var out [20]byte
	if !IsAddress(s) {
		return out, fmt.Errorf("not an address: %q", s)
	}
	raw, _ := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
	copy(out[:], raw)
	return out, nil
}

// ParseUsdcAmount turns a dollar string ("25", "12.50", "0.000001") into
// micro-USDC. Rejects more than six decimals, negatives, and zero.
func ParseUsdcAmount(s string) (*big.Int, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "$"))
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if len(frac) > UsdcDecimals {
		return nil, fmt.Errorf("USDC has at most %d decimal places: %q", UsdcDecimals, s)
	}
	frac += strings.Repeat("0", UsdcDecimals-len(frac))
	digits := whole + frac
	for _, c := range digits {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("not a dollar amount: %q", s)
		}
	}
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok || v.Sign() <= 0 {
		return nil, fmt.Errorf("amount must be positive: %q", s)
	}
	return v, nil
}

// FormatUsdc renders micro-USDC as a dollar string with two decimals (more
// only when the amount has sub-cent precision).
func FormatUsdc(micros *big.Int) string {
	if micros == nil {
		return "0.00"
	}
	neg := micros.Sign() < 0
	abs := new(big.Int).Abs(micros)
	whole, frac := new(big.Int).DivMod(abs, big.NewInt(1_000_000), new(big.Int))
	fracStr := fmt.Sprintf("%06d", frac.Int64())
	if strings.HasSuffix(fracStr, "0000") {
		fracStr = fracStr[:2]
	} else {
		fracStr = strings.TrimRight(fracStr, "0")
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%s", sign, whole.String(), fracStr)
}

// FormatEth renders wei as ETH: up to 6 decimals normally, but never
// rounding a non-zero amount down to "0" — a Base gas budget is a few
// hundred gwei (~1e-7 ETH), and telling someone they "need up to 0 ETH" is
// worse than showing 0.00000042.
func FormatEth(wei *big.Int) string {
	if wei == nil || wei.Sign() == 0 {
		return "0"
	}
	whole, frac := new(big.Int).DivMod(wei, big.NewInt(1e18), new(big.Int))
	full := fmt.Sprintf("%018d", frac)
	fracStr := strings.TrimRight(full[:6], "0")
	if whole.Sign() == 0 && fracStr == "" {
		// Sub-microether: keep enough digits to show the first non-zero one.
		fracStr = strings.TrimRight(full, "0")
	}
	if fracStr == "" {
		return whole.String()
	}
	return whole.String() + "." + fracStr
}
