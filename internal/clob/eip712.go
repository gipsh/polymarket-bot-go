// Package clob: EIP-712 typed-data signing for Polymarket CLOB orders.
//
// Polymarket uses EIP-712 for all order signatures.
// Domain: "Polymarket CTF Exchange" (or Neg Risk CTF Exchange)
// Order struct is hashed per EIP-712 spec, then signed with the EOA key.
// For Gnosis Safe (SIGNATURE_TYPE=2), the signature bytes end with \x02.
package clob

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ── Contract addresses (Polygon mainnet) ────────────────────────────────

const (
	CTFExchangeAddr        = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	NegRiskCTFExchangeAddr = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
	NegRiskAdapterAddr     = "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296"
)

// ── EIP-712 type hashes ──────────────────────────────────────────────────
// Computed once at startup: keccak256 of the type string.

var (
	// keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")
	domainTypeHash = mustKeccak([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))

	// keccak256("Order(uint256 salt,address maker,address signer,address taker,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint256 expiration,uint256 nonce,uint256 feeRateBps,uint8 side,uint8 signatureType)")
	orderTypeHash = mustKeccak([]byte("Order(uint256 salt,address maker,address signer,address taker,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint256 expiration,uint256 nonce,uint256 feeRateBps,uint8 side,uint8 signatureType)"))
)

// OrderParams holds the raw fields needed to build a CLOB order.
type OrderParams struct {
	Salt          *big.Int
	Maker         common.Address // order maker (EOA or Safe)
	Signer        common.Address // actual signer (EOA)
	Taker         common.Address // zero address = open order
	TokenID       *big.Int       // token ID (uint256)
	MakerAmount   *big.Int       // USDC in (6 decimals)
	TakerAmount   *big.Int       // tokens out (6 decimals)
	Expiration    *big.Int       // 0 = no expiry
	Nonce         *big.Int       // 0 for market orders
	FeeRateBps    *big.Int       // 0 for taker orders
	Side          uint8          // 0=BUY, 1=SELL
	SignatureType uint8          // 0=EOA, 1=PolyProxy, 2=GnosisSafe
}

// BuildAndSignOrder builds the EIP-712 digest, signs it with key, and
// returns the hex-encoded signature (with 0-padded v and sig-type byte
// appended for GnosisSafe).
//
// isNegRisk selects the NegRisk CTF Exchange domain.
func BuildAndSignOrder(params OrderParams, key *ecdsa.PrivateKey, isNegRisk bool) (string, error) {
	// 1. Build domain separator
	domainSep := buildDomainSeparator(isNegRisk)

	// 2. Build struct hash
	structHash := buildOrderStructHash(params)

	// 3. EIP-712 digest: 0x1901 + domainSep + structHash
	digest := crypto.Keccak256(
		append([]byte{0x19, 0x01}, append(domainSep, structHash...)...),
	)

	// 4. Sign
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	// go-ethereum returns [R(32) | S(32) | V(1)] where V is 0 or 1.
	// EIP-712 expects V as 27 or 28.
	sig[64] += 27

	// 5. For Gnosis Safe, append \x02
	if params.SignatureType == 2 {
		sig = append(sig, 0x02)
	}

	return "0x" + hex.EncodeToString(sig), nil
}

// ── Domain separator ──────────────────────────────────────────────────────

func buildDomainSeparator(isNegRisk bool) []byte {
	name := "Polymarket CTF Exchange"
	contractHex := CTFExchangeAddr
	if isNegRisk {
		name = "Polymarket Neg Risk CTF Exchange"
		contractHex = NegRiskCTFExchangeAddr
	}

	nameHash    := crypto.Keccak256([]byte(name))
	versionHash := crypto.Keccak256([]byte("1"))
	chainID     := padUint256(big.NewInt(137))
	contract    := padAddress(common.HexToAddress(contractHex))

	encoded := make([]byte, 0, 32*5)
	encoded = append(encoded, domainTypeHash...)
	encoded = append(encoded, nameHash...)
	encoded = append(encoded, versionHash...)
	encoded = append(encoded, chainID...)
	encoded = append(encoded, contract...)

	return crypto.Keccak256(encoded)
}

// ── Order struct hash ─────────────────────────────────────────────────────

func buildOrderStructHash(p OrderParams) []byte {
	encoded := make([]byte, 0, 32*13)
	encoded = append(encoded, orderTypeHash...)
	encoded = append(encoded, padUint256(p.Salt)...)
	encoded = append(encoded, padAddress(p.Maker)...)
	encoded = append(encoded, padAddress(p.Signer)...)
	encoded = append(encoded, padAddress(p.Taker)...)
	encoded = append(encoded, padUint256(p.TokenID)...)
	encoded = append(encoded, padUint256(p.MakerAmount)...)
	encoded = append(encoded, padUint256(p.TakerAmount)...)
	encoded = append(encoded, padUint256(p.Expiration)...)
	encoded = append(encoded, padUint256(p.Nonce)...)
	encoded = append(encoded, padUint256(p.FeeRateBps)...)
	encoded = append(encoded, padUint8(p.Side)...)
	encoded = append(encoded, padUint8(p.SignatureType)...)
	return crypto.Keccak256(encoded)
}

// ── ABI-encoding helpers ──────────────────────────────────────────────────

// padUint256 ABI-encodes a *big.Int as a 32-byte big-endian value.
func padUint256(n *big.Int) []byte {
	if n == nil {
		n = big.NewInt(0)
	}
	b := n.Bytes()
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// padAddress ABI-encodes an address as a 32-byte value (left-padded with zeros).
func padAddress(addr common.Address) []byte {
	padded := make([]byte, 32)
	copy(padded[12:], addr[:])
	return padded
}

// padUint8 ABI-encodes a uint8 as a 32-byte value.
func padUint8(n uint8) []byte {
	padded := make([]byte, 32)
	padded[31] = n
	return padded
}

// mustKeccak computes keccak256 and panics on nil input (never happens in practice).
func mustKeccak(data []byte) []byte {
	return crypto.Keccak256(data)
}

// ── Personal sign (L1 auth) ───────────────────────────────────────────────

// PersonalSign creates an Ethereum personal_sign signature over the given message.
// Used for L1 API credential creation (signing the timestamp string).
func PersonalSign(message string, key *ecdsa.PrivateKey) (string, error) {
	// Ethereum personal sign: keccak256("\x19Ethereum Signed Message:\n{len(msg)}{msg}")
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	hash := crypto.Keccak256([]byte(prefix + message))

	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return "", fmt.Errorf("personalSign: %w", err)
	}
	sig[64] += 27
	return "0x" + hex.EncodeToString(sig), nil
}

// ── Key helpers ───────────────────────────────────────────────────────────

// ParsePrivateKey parses a hex private key string (with or without 0x prefix).
func ParsePrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	hexKey = strings.TrimPrefix(hexKey, "0x")
	return crypto.HexToECDSA(hexKey)
}

// AddressFromKey returns the Ethereum address for a given private key.
func AddressFromKey(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}

// TokenIDFromHex parses a token ID from a hex or decimal string.
// Polymarket token IDs are large uint256 values (decimal strings in the API).
func TokenIDFromHex(s string) (*big.Int, error) {
	s = strings.TrimPrefix(s, "0x")
	n := new(big.Int)
	if _, ok := n.SetString(s, 10); ok {
		return n, nil
	}
	if _, ok := n.SetString(s, 16); ok {
		return n, nil
	}
	return nil, fmt.Errorf("invalid token ID: %q", s)
}

// USDCToUnits converts a USDC float amount to its uint256 representation (6 decimals).
func USDCToUnits(usdc float64) *big.Int {
	units := int64(usdc * 1e6)
	return big.NewInt(units)
}
