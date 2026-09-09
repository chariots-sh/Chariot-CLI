package wallet

import "math/big"

// Minimal RLP encoder — just enough for an EIP-1559 transaction.

func rlpBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	return append(rlpLength(len(b), 0x80), b...)
}

func rlpList(items ...[]byte) []byte {
	var body []byte
	for _, it := range items {
		body = append(body, it...)
	}
	return append(rlpLength(len(body), 0xc0), body...)
}

func rlpLength(n int, offset byte) []byte {
	if n < 56 {
		return []byte{offset + byte(n)}
	}
	be := big.NewInt(int64(n)).Bytes()
	return append([]byte{offset + 55 + byte(len(be))}, be...)
}

// rlpUint encodes an integer as its minimal big-endian bytes (0 → empty).
func rlpUint(v *big.Int) []byte {
	if v == nil || v.Sign() == 0 {
		return rlpBytes(nil)
	}
	return rlpBytes(v.Bytes())
}
