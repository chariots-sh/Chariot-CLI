package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseRPC is the public Base mainnet endpoint; override with
// CHARIOT_BASE_RPC_URL or --rpc when it rate-limits you.
const DefaultBaseRPC = "https://mainnet.base.org"

// EnvRPC names the env var overriding the Base JSON-RPC endpoint.
const EnvRPC = "CHARIOT_BASE_RPC_URL"

// RPC is a minimal Ethereum JSON-RPC client.
type RPC struct {
	URL  string
	HTTP *http.Client
}

// NewRPC builds a client with a sane timeout.
func NewRPC(url string) *RPC {
	return &RPC{URL: url, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r *RPC) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: RPC HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out rpcResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: decoding RPC response: %w", method, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s (RPC error %d)", method, out.Error.Message, out.Error.Code)
	}
	return out.Result, nil
}

func (r *RPC) callQuantity(ctx context.Context, method string, params ...any) (*big.Int, error) {
	raw, err := r.call(ctx, method, params...)
	if err != nil {
		return nil, err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: result is not a quantity: %s", method, raw)
	}
	return parseQuantity(s)
}

func parseQuantity(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("not a hex quantity: %q", s)
	}
	return v, nil
}

// ChainID returns eth_chainId.
func (r *RPC) ChainID(ctx context.Context) (*big.Int, error) {
	return r.callQuantity(ctx, "eth_chainId")
}

// PendingNonce returns the account's next nonce (pending tag).
func (r *RPC) PendingNonce(ctx context.Context, address string) (uint64, error) {
	v, err := r.callQuantity(ctx, "eth_getTransactionCount", address, "pending")
	if err != nil {
		return 0, err
	}
	return v.Uint64(), nil
}

// EthBalance returns the native balance in wei.
func (r *RPC) EthBalance(ctx context.Context, address string) (*big.Int, error) {
	return r.callQuantity(ctx, "eth_getBalance", address, "latest")
}

// UsdcBalance calls balanceOf on the token contract.
func (r *RPC) UsdcBalance(ctx context.Context, token string, owner [20]byte) (*big.Int, error) {
	call := map[string]string{"to": token, "data": "0x" + hex.EncodeToString(BalanceOfCalldata(owner))}
	return r.callQuantity(ctx, "eth_call", call, "latest")
}

// Fees returns (maxPriorityFeePerGas, maxFeePerGas) for a transaction that
// should land promptly: the node's suggested tip and twice the latest base
// fee plus the tip (headroom for a few blocks of base-fee growth; the
// unused part is never charged).
func (r *RPC) Fees(ctx context.Context) (tip, maxFee *big.Int, err error) {
	tip, err = r.callQuantity(ctx, "eth_maxPriorityFeePerGas")
	if err != nil {
		tip = big.NewInt(1_000_000) // 0.001 gwei — nodes without the method
	}
	raw, err := r.call(ctx, "eth_getBlockByNumber", "latest", false)
	if err != nil {
		return nil, nil, err
	}
	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := json.Unmarshal(raw, &block); err != nil || block.BaseFeePerGas == "" {
		return nil, nil, fmt.Errorf("eth_getBlockByNumber: no baseFeePerGas in latest block")
	}
	base, err := parseQuantity(block.BaseFeePerGas)
	if err != nil {
		return nil, nil, err
	}
	maxFee = new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	return tip, maxFee, nil
}

// EstimateGas returns eth_estimateGas for a call.
func (r *RPC) EstimateGas(ctx context.Context, from, to string, data []byte) (uint64, error) {
	call := map[string]string{"from": from, "to": to, "data": "0x" + hex.EncodeToString(data)}
	v, err := r.callQuantity(ctx, "eth_estimateGas", call)
	if err != nil {
		return 0, err
	}
	return v.Uint64(), nil
}

// SendRaw broadcasts a signed transaction and returns its hash.
func (r *RPC) SendRaw(ctx context.Context, rawTx string) (string, error) {
	raw, err := r.call(ctx, "eth_sendRawTransaction", rawTx)
	if err != nil {
		return "", err
	}
	var hash string
	if err := json.Unmarshal(raw, &hash); err != nil {
		return "", fmt.Errorf("eth_sendRawTransaction: unexpected result %s", raw)
	}
	return hash, nil
}

// Receipt is the subset of a transaction receipt the CLI reads.
type Receipt struct {
	Status      string `json:"status"`
	BlockNumber string `json:"blockNumber"`
}

// TransactionReceipt returns the receipt, or nil while the tx is unmined.
func (r *RPC) TransactionReceipt(ctx context.Context, hash string) (*Receipt, error) {
	raw, err := r.call(ctx, "eth_getTransactionReceipt", hash)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return nil, nil
	}
	var out Receipt
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding receipt: %w", err)
	}
	return &out, nil
}

// SendUsdc signs and broadcasts an ERC-20 transfer of amountMicros USDC from
// k to `to`, checking the chain id, USDC balance, and ETH-for-gas balance
// first so a doomed transaction is refused before anything is signed.
func SendUsdc(ctx context.Context, rpc *RPC, k *Key, chainID int64, token string, to string, amountMicros *big.Int) (txHash string, err error) {
	got, err := rpc.ChainID(ctx)
	if err != nil {
		return "", err
	}
	if got.Int64() != chainID {
		return "", fmt.Errorf("RPC %s is chain %d, but the Chariot treasury is on chain %d", rpc.URL, got.Int64(), chainID)
	}
	toAddr, err := ParseAddress(to)
	if err != nil {
		return "", err
	}
	tokenAddr, err := ParseAddress(token)
	if err != nil {
		return "", err
	}
	from := k.Address()
	balance, err := rpc.UsdcBalance(ctx, token, k.AddressBytes())
	if err != nil {
		return "", err
	}
	if balance.Cmp(amountMicros) < 0 {
		return "", fmt.Errorf("wallet %s holds %s USDC, less than the %s requested — send USDC on Base to it first",
			from, FormatUsdc(balance), FormatUsdc(amountMicros))
	}
	data := TransferCalldata(toAddr, amountMicros)
	gas, err := rpc.EstimateGas(ctx, from, token, data)
	if err != nil {
		return "", fmt.Errorf("estimating gas: %w", err)
	}
	gas = gas + gas/5 // 20% headroom; unused gas is refunded
	tip, maxFee, err := rpc.Fees(ctx)
	if err != nil {
		return "", err
	}
	needWei := new(big.Int).Mul(maxFee, new(big.Int).SetUint64(gas))
	ethBalance, err := rpc.EthBalance(ctx, from)
	if err != nil {
		return "", err
	}
	if ethBalance.Cmp(needWei) < 0 {
		return "", fmt.Errorf("wallet %s holds %s ETH on Base but the transfer needs up to %s ETH for gas — send a little ETH to it first",
			from, FormatEth(ethBalance), FormatEth(needWei))
	}
	nonce, err := rpc.PendingNonce(ctx, from)
	if err != nil {
		return "", err
	}
	tx := &Eip1559Tx{
		ChainID:              big.NewInt(chainID),
		Nonce:                nonce,
		MaxPriorityFeePerGas: tip,
		MaxFeePerGas:         maxFee,
		Gas:                  gas,
		To:                   tokenAddr,
		Value:                big.NewInt(0),
		Data:                 data,
	}
	raw, _ := tx.Sign(k)
	return rpc.SendRaw(ctx, raw)
}
