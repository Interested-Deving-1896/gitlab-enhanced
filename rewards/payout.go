package rewards

// ERC-20 non-custodial payout via Ethereum JSON-RPC.
//
// This file implements direct BAT (ERC-20) transfers using raw JSON-RPC calls
// to an Ethereum node (local geth, Infura, Alchemy, etc.). No go-ethereum
// dependency is required — the ABI encoding for a simple ERC-20 transfer is
// fixed and can be constructed by hand.
//
// BAT contract address (Ethereum mainnet): 0x0D8775F648430679A709E98d2b0Cb6250d2887EF
// Transfer function selector: keccak256("transfer(address,uint256)")[:4] = 0xa9059cbb
//
// The non-custodial path requires:
//   - rewards.eth_rpc_url: Ethereum JSON-RPC endpoint (e.g. https://mainnet.infura.io/v3/<key>)
//   - rewards.eth_private_key: hex-encoded private key of the publisher wallet (no 0x prefix)
//   - rewards.wallet_address: the publisher's Ethereum address (must hold BAT)
//
// The custodial Uphold path (upholdToken + submitPayout in service.go) is used
// when uphold_client_id is set. The non-custodial path is used as fallback when
// only eth_rpc_url + eth_private_key are configured.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"math/big"
	"net/http"
	"strings"
	"time"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	dcrecdsa "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// batContractAddress is the BAT ERC-20 contract on Ethereum mainnet.
const batContractAddress = "0x0D8775F648430679A709E98d2b0Cb6250d2887EF"

// transferSelector is the first 4 bytes of keccak256("transfer(address,uint256)").
// Pre-computed: 0xa9059cbb
var transferSelector = []byte{0xa9, 0x05, 0x9c, 0xbb}

// EthConfig holds the Ethereum RPC configuration for non-custodial payouts.
type EthConfig struct {
	// RPCURL is the Ethereum JSON-RPC endpoint.
	// Examples: https://mainnet.infura.io/v3/<key>, http://localhost:8545
	RPCURL string
	// PrivateKeyHex is the hex-encoded secp256k1 private key (no 0x prefix).
	// This key must control the wallet at WalletAddress and hold sufficient BAT.
	PrivateKeyHex string
	// WalletAddress is the Ethereum address of the publisher wallet (0x-prefixed).
	WalletAddress string
	// ChainID is the Ethereum chain ID (1 = mainnet, 5 = Goerli, 11155111 = Sepolia).
	// Defaults to 1 (mainnet) when zero.
	ChainID int64
}

// submitERC20Payout sends BAT directly on-chain to the recipient address.
// Returns the transaction hash on success.
//
// The transaction is constructed, signed with secp256k1, and submitted via
// eth_sendRawTransaction. No external library is required — ERC-20 transfer
// ABI encoding is fixed for a two-argument call (address, uint256).
func submitERC20Payout(cfg EthConfig, toAddress string, amountBAT float64) (string, error) {
	if cfg.RPCURL == "" {
		return "", fmt.Errorf("eth_rpc_url is not configured")
	}
	if cfg.PrivateKeyHex == "" {
		return "", fmt.Errorf("eth_private_key is not configured")
	}
	if cfg.ChainID == 0 {
		cfg.ChainID = 1
	}

	privKey, err := parsePrivKey(cfg.PrivateKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid eth_private_key: %w", err)
	}

	// Convert BAT amount to wei (BAT has 18 decimals).
	amountWei := batToWei(amountBAT)

	// Build the ERC-20 transfer calldata:
	//   selector (4 bytes) + address (32 bytes, left-padded) + amount (32 bytes, big-endian)
	data := encodeTransferCall(toAddress, amountWei)

	// Fetch nonce and gas price from the node.
	nonce, err := ethGetNonce(cfg.RPCURL, cfg.WalletAddress)
	if err != nil {
		return "", fmt.Errorf("eth_getTransactionCount: %w", err)
	}
	gasPrice, err := ethGasPrice(cfg.RPCURL)
	if err != nil {
		return "", fmt.Errorf("eth_gasPrice: %w", err)
	}

	// ERC-20 transfers use ~65 000 gas; add 20% headroom.
	gasLimit := uint64(78000)

	// Build and sign the legacy (type-0) transaction.
	rawTx, err := signLegacyTx(privKey, cfg.ChainID, nonce, gasPrice, gasLimit,
		batContractAddress, big.NewInt(0), data)
	if err != nil {
		return "", fmt.Errorf("signing transaction: %w", err)
	}

	txHash, err := ethSendRawTransaction(cfg.RPCURL, rawTx)
	if err != nil {
		return "", fmt.Errorf("eth_sendRawTransaction: %w", err)
	}
	return txHash, nil
}

// --- ABI encoding ---

// encodeTransferCall builds the calldata for ERC-20 transfer(address,uint256).
func encodeTransferCall(toAddress string, amount *big.Int) []byte {
	// Strip 0x prefix and decode address (20 bytes).
	addrHex := strings.TrimPrefix(strings.ToLower(toAddress), "0x")
	addrBytes, _ := hex.DecodeString(addrHex)

	// ABI encoding: each argument is 32 bytes, right-aligned.
	var buf [68]byte // 4 (selector) + 32 (address) + 32 (amount)
	copy(buf[:4], transferSelector)
	// Address: left-pad to 32 bytes (12 zero bytes + 20 address bytes)
	copy(buf[4+12:4+32], addrBytes)
	// Amount: big-endian uint256 in the last 32 bytes
	amountBytes := amount.Bytes()
	copy(buf[4+32+(32-len(amountBytes)):], amountBytes)
	return buf[:]
}

// batToWei converts a BAT float64 amount to wei (10^18 units).
func batToWei(bat float64) *big.Int {
	// Multiply by 10^18 using big.Float to avoid float64 precision loss.
	f := new(big.Float).SetFloat64(bat)
	decimals := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	f.Mul(f, decimals)
	result, _ := f.Int(nil)
	return result
}

// --- JSON-RPC helpers ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type jsonRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func ethCall(rpcURL, method string, params []any) (json.RawMessage, error) {
	body, _ := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func ethGetNonce(rpcURL, address string) (uint64, error) {
	result, err := ethCall(rpcURL, "eth_getTransactionCount", []any{address, "pending"})
	if err != nil {
		return 0, err
	}
	var hexNonce string
	if err := json.Unmarshal(result, &hexNonce); err != nil {
		return 0, err
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(hexNonce, "0x"), 16)
	return n.Uint64(), nil
}

func ethGasPrice(rpcURL string) (*big.Int, error) {
	result, err := ethCall(rpcURL, "eth_gasPrice", []any{})
	if err != nil {
		return nil, err
	}
	var hexPrice string
	if err := json.Unmarshal(result, &hexPrice); err != nil {
		return nil, err
	}
	p := new(big.Int)
	p.SetString(strings.TrimPrefix(hexPrice, "0x"), 16)
	return p, nil
}

func ethSendRawTransaction(rpcURL string, rawTx []byte) (string, error) {
	result, err := ethCall(rpcURL, "eth_sendRawTransaction",
		[]any{"0x" + hex.EncodeToString(rawTx)})
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", err
	}
	return txHash, nil
}

// --- Transaction signing (EIP-155 legacy transaction) ---

// signLegacyTx constructs and signs an EIP-155 legacy transaction.
// Returns the RLP-encoded signed transaction bytes.
func signLegacyTx(
	key *secp256k1.PrivateKey,
	chainID int64,
	nonce uint64,
	gasPrice *big.Int,
	gasLimit uint64,
	to string,
	value *big.Int,
	data []byte,
) ([]byte, error) {
	// RLP-encode the unsigned transaction for signing (EIP-155):
	// [nonce, gasPrice, gasLimit, to, value, data, chainID, 0, 0]
	toBytes, _ := hex.DecodeString(strings.TrimPrefix(to, "0x"))

	unsigned := rlpEncodeList([][]byte{
		rlpEncodeUint(nonce),
		rlpEncodeBigInt(gasPrice),
		rlpEncodeUint(gasLimit),
		toBytes,
		rlpEncodeBigInt(value),
		data,
		rlpEncodeUint(uint64(chainID)),
		rlpEncodeUint(0),
		rlpEncodeUint(0),
	})

	// Sign the keccak256 hash of the RLP-encoded transaction.
	hash := keccak256(unsigned)
	r, s, v, err := ecdsaSign(key, hash, chainID)
	if err != nil {
		return nil, err
	}

	// RLP-encode the signed transaction: [nonce, gasPrice, gasLimit, to, value, data, v, r, s]
	signed := rlpEncodeList([][]byte{
		rlpEncodeUint(nonce),
		rlpEncodeBigInt(gasPrice),
		rlpEncodeUint(gasLimit),
		toBytes,
		rlpEncodeBigInt(value),
		data,
		rlpEncodeBigInt(v),
		rlpEncodeBigInt(r),
		rlpEncodeBigInt(s),
	})
	return signed, nil
}

// ecdsaSign signs hash with key using constant-time secp256k1 (decred/dcrd)
// and returns (r, s, v) for EIP-155 replay protection.
// v = chainID*2 + 35 + recovery_bit.
func ecdsaSign(key *secp256k1.PrivateKey, msgHash []byte, chainID int64) (*big.Int, *big.Int, *big.Int, error) {
	// Sign using RFC6979 deterministic nonce (constant-time, no rand.Reader).
	sig := dcrecdsa.SignCompact(key, msgHash, false)
	// SignCompact returns: [recovery_flag(1)] [r(32)] [s(32)]
	if len(sig) != 65 {
		return nil, nil, nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	// The first byte encodes the recovery ID (27 or 28 for uncompressed).
	recoveryBit := int64(sig[0] - 27)
	r := new(big.Int).SetBytes(sig[1:33])
	s := new(big.Int).SetBytes(sig[33:65])

	// EIP-155: v = chainID * 2 + 35 + recoveryBit
	v := new(big.Int).SetInt64(chainID*2 + 35 + recoveryBit)
	return r, s, v, nil
}

// --- Minimal RLP encoding ---

func rlpEncodeUint(n uint64) []byte {
	if n == 0 {
		return []byte{0x80} // RLP empty string
	}
	b := new(big.Int).SetUint64(n).Bytes()
	return rlpEncodeBytes(b)
}

func rlpEncodeBigInt(n *big.Int) []byte {
	if n == nil || n.Sign() == 0 {
		return []byte{0x80}
	}
	return rlpEncodeBytes(n.Bytes())
}

func rlpEncodeBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	return append(rlpLengthPrefix(len(b), 0x80), b...)
}

func rlpEncodeList(items [][]byte) []byte {
	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	return append(rlpLengthPrefix(len(payload), 0xc0), payload...)
}

func rlpLengthPrefix(length, offset int) []byte {
	if length < 56 {
		return []byte{byte(offset + length)}
	}
	lenBytes := new(big.Int).SetInt64(int64(length)).Bytes()
	return append([]byte{byte(offset + 55 + len(lenBytes))}, lenBytes...)
}

// --- Crypto helpers ---

// keccak256 computes the Keccak-256 hash (not SHA3-256 — Ethereum uses the
// pre-standardisation variant). We implement it using Go's sha3 package with
// the legacy padding.
func keccak256(data []byte) []byte {
	h := newKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// newKeccak256 returns a hash.Hash implementing Keccak-256 (the pre-NIST
// variant used by Ethereum — distinct from SHA3-256).
// Uses golang.org/x/crypto/sha3.NewLegacyKeccak256().
func newKeccak256() hash.Hash {
	return sha3.NewLegacyKeccak256()
}

// parsePrivKey parses a hex-encoded secp256k1 private key (with or without 0x prefix).
func parsePrivKey(hexKey string) (*secp256k1.PrivateKey, error) {
	hexKey = strings.TrimPrefix(hexKey, "0x")
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(b))
	}
	return secp256k1.PrivKeyFromBytes(b), nil
}
