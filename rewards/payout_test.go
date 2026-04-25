package rewards

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

func TestEncodeTransferCall_Length(t *testing.T) {
	data := encodeTransferCall("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", big.NewInt(1e18))
	if len(data) != 68 {
		t.Errorf("expected 68 bytes, got %d", len(data))
	}
}

func TestEncodeTransferCall_Selector(t *testing.T) {
	data := encodeTransferCall("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", big.NewInt(1))
	// First 4 bytes must be the transfer(address,uint256) selector: 0xa9059cbb
	if hex.EncodeToString(data[:4]) != "a9059cbb" {
		t.Errorf("wrong selector: %s", hex.EncodeToString(data[:4]))
	}
}

func TestEncodeTransferCall_AddressPadding(t *testing.T) {
	addr := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	data := encodeTransferCall(addr, big.NewInt(1))
	// Bytes 4-15 (12 bytes) must be zero padding for the address.
	for i := 4; i < 16; i++ {
		if data[i] != 0 {
			t.Errorf("expected zero padding at byte %d, got 0x%02x", i, data[i])
		}
	}
	// Bytes 16-35 must contain the 20-byte address.
	addrHex := strings.ToLower(strings.TrimPrefix(addr, "0x"))
	encoded := hex.EncodeToString(data[16:36])
	if encoded != addrHex {
		t.Errorf("address mismatch: want %s, got %s", addrHex, encoded)
	}
}

func TestEncodeTransferCall_Amount(t *testing.T) {
	// 1 BAT = 1e18 wei
	amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	data := encodeTransferCall("0x0000000000000000000000000000000000000001", amount)
	// Last 32 bytes encode the amount big-endian.
	encoded := new(big.Int).SetBytes(data[36:68])
	if encoded.Cmp(amount) != 0 {
		t.Errorf("amount mismatch: want %s, got %s", amount, encoded)
	}
}

func TestBatToWei_OneBAT(t *testing.T) {
	wei := batToWei(1.0)
	expected := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if wei.Cmp(expected) != 0 {
		t.Errorf("1 BAT: want %s wei, got %s", expected, wei)
	}
}

func TestBatToWei_FractionalBAT(t *testing.T) {
	wei := batToWei(0.25)
	// 0.25 BAT = 25e16 wei
	expected := new(big.Int).Mul(big.NewInt(25), new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil))
	if wei.Cmp(expected) != 0 {
		t.Errorf("0.25 BAT: want %s wei, got %s", expected, wei)
	}
}

func TestRLPEncodeUint_Zero(t *testing.T) {
	b := rlpEncodeUint(0)
	if len(b) != 1 || b[0] != 0x80 {
		t.Errorf("RLP(0): want [0x80], got %x", b)
	}
}

func TestRLPEncodeUint_SmallValue(t *testing.T) {
	// Values < 0x80 encode as a single byte.
	b := rlpEncodeUint(1)
	if len(b) != 1 || b[0] != 0x01 {
		t.Errorf("RLP(1): want [0x01], got %x", b)
	}
}

func TestRLPEncodeList_Empty(t *testing.T) {
	b := rlpEncodeList(nil)
	// Empty list encodes as 0xc0.
	if len(b) != 1 || b[0] != 0xc0 {
		t.Errorf("RLP([]): want [0xc0], got %x", b)
	}
}

func TestParsePrivKey_InvalidLength(t *testing.T) {
	_, err := parsePrivKey("deadbeef") // too short
	if err == nil {
		t.Error("expected error for short key, got nil")
	}
}

func TestParsePrivKey_InvalidHex(t *testing.T) {
	_, err := parsePrivKey("zzzz")
	if err == nil {
		t.Error("expected error for invalid hex, got nil")
	}
}

func TestSubmitERC20Payout_MissingRPCURL(t *testing.T) {
	_, err := submitERC20Payout(EthConfig{
		PrivateKeyHex: strings.Repeat("ab", 32),
		WalletAddress: "0x1234",
	}, "0x5678", 1.0)
	if err == nil || !strings.Contains(err.Error(), "eth_rpc_url") {
		t.Errorf("expected eth_rpc_url error, got: %v", err)
	}
}

func TestSubmitERC20Payout_MissingPrivateKey(t *testing.T) {
	_, err := submitERC20Payout(EthConfig{
		RPCURL:        "http://localhost:8545",
		WalletAddress: "0x1234",
	}, "0x5678", 1.0)
	if err == nil || !strings.Contains(err.Error(), "eth_private_key") {
		t.Errorf("expected eth_private_key error, got: %v", err)
	}
}

// TestKeccak256_KnownVector verifies the hash function is real Keccak-256
// (not SHA-256). The empty-string Keccak-256 digest is a well-known constant.
func TestKeccak256_KnownVector(t *testing.T) {
	// Keccak-256("") = c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
	// SHA-256("")    = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	got := hex.EncodeToString(keccak256([]byte{}))
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	if got != want {
		t.Errorf("keccak256(\"\") = %s, want %s\n(if this looks like SHA-256, the placeholder was not replaced)", got, want)
	}
}

// TestKeccak256_EthereumAddress verifies keccak256 against the well-known
// Ethereum address derivation: keccak256 of the secp256k1 public key bytes.
func TestKeccak256_NonEmpty(t *testing.T) {
	// keccak256("abc") = 4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45
	got := hex.EncodeToString(keccak256([]byte("abc")))
	want := "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
	if got != want {
		t.Errorf("keccak256(\"abc\") = %s, want %s", got, want)
	}
}
