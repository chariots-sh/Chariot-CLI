package wallet

import (
	"encoding/hex"
	"math/big"
)

// Eip1559Tx is a type-2 (dynamic fee) transaction with an empty access list.
type Eip1559Tx struct {
	ChainID              *big.Int
	Nonce                uint64
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	Gas                  uint64
	To                   [20]byte
	Value                *big.Int
	Data                 []byte
}

func (t *Eip1559Tx) fields() [][]byte {
	return [][]byte{
		rlpUint(t.ChainID),
		rlpUint(new(big.Int).SetUint64(t.Nonce)),
		rlpUint(t.MaxPriorityFeePerGas),
		rlpUint(t.MaxFeePerGas),
		rlpUint(new(big.Int).SetUint64(t.Gas)),
		rlpBytes(t.To[:]),
		rlpUint(t.Value),
		rlpBytes(t.Data),
		rlpList(), // access list
	}
}

// Sign returns the signed raw transaction as 0x-hex, ready for
// eth_sendRawTransaction, plus its hash.
func (t *Eip1559Tx) Sign(k *Key) (raw string, hash string) {
	unsigned := append([]byte{0x02}, rlpList(t.fields()...)...)
	sig := k.signRecoverable(Keccak256(unsigned), 0) // v = yParity (0/1)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	y := new(big.Int).SetUint64(uint64(sig[64]))
	signed := append([]byte{0x02}, rlpList(append(t.fields(), rlpUint(y), rlpUint(r), rlpUint(s))...)...)
	return "0x" + hex.EncodeToString(signed), "0x" + hex.EncodeToString(Keccak256(signed))
}
