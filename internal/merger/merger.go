// Package merger executes on-chain MERGE (mergePositions) via the Gnosis Safe.
// Mirror of Python merger.py.
//
// Architecture:
//   MetaMask EOA (MERGE_PRIVATE_KEY) → signs execTransaction on Gnosis Safe
//   Gnosis Safe (FUNDER_ADDRESS) → calls ConditionalTokens.mergePositions()
//   ConditionalTokens → burns UP+DOWN tokens → returns USDC to Safe
package merger

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/gipsh/polymarket-bot-go/internal/clob"
	"github.com/gipsh/polymarket-bot-go/internal/config"
)

// ── Contract addresses (Polygon mainnet) ────────────────────────────────

var (
	conditionalTokensAddr = common.HexToAddress("0x4D97DCd97eC945f40cF65F87097ACe5EA0476045")
	usdcAddr              = common.HexToAddress("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174")
	gnosisSafeMasterCopy  = "1.3.0"
)

// ── ABIs ─────────────────────────────────────────────────────────────────

const conditionalTokensABI = `[{
	"name":"mergePositions",
	"type":"function",
	"inputs":[
		{"name":"collateralToken","type":"address"},
		{"name":"parentCollectionId","type":"bytes32"},
		{"name":"conditionId","type":"bytes32"},
		{"name":"partition","type":"uint256[]"},
		{"name":"amount","type":"uint256"}
	],
	"outputs":[]
},{
	"name":"balanceOf",
	"type":"function",
	"inputs":[
		{"name":"owner","type":"address"},
		{"name":"id","type":"uint256"}
	],
	"outputs":[{"name":"","type":"uint256"}]
},{
	"name":"getConditionId",
	"type":"function",
	"inputs":[
		{"name":"oracle","type":"address"},
		{"name":"questionId","type":"bytes32"},
		{"name":"outcomeSlotCount","type":"uint256"}
	],
	"outputs":[{"name":"","type":"bytes32"}]
}]`

const gnosisSafeABI = `[{
	"name":"execTransaction",
	"type":"function",
	"inputs":[
		{"name":"to","type":"address"},
		{"name":"value","type":"uint256"},
		{"name":"data","type":"bytes"},
		{"name":"operation","type":"uint8"},
		{"name":"safeTxGas","type":"uint256"},
		{"name":"baseGas","type":"uint256"},
		{"name":"gasPrice","type":"uint256"},
		{"name":"gasToken","type":"address"},
		{"name":"refundReceiver","type":"address"},
		{"name":"signatures","type":"bytes"}
	],
	"outputs":[{"name":"","type":"bool"}]
},{
	"name":"getTransactionHash",
	"type":"function",
	"inputs":[
		{"name":"to","type":"address"},
		{"name":"value","type":"uint256"},
		{"name":"data","type":"bytes"},
		{"name":"operation","type":"uint8"},
		{"name":"safeTxGas","type":"uint256"},
		{"name":"baseGas","type":"uint256"},
		{"name":"gasPrice","type":"uint256"},
		{"name":"gasToken","type":"address"},
		{"name":"refundReceiver","type":"address"},
		{"name":"_nonce","type":"uint256"}
	],
	"outputs":[{"name":"","type":"bytes32"}]
},{
	"name":"nonce",
	"type":"function",
	"inputs":[],
	"outputs":[{"name":"","type":"uint256"}]
}]`

// Merger handles on-chain mergePositions via the Gnosis Safe.
type Merger struct {
	ready    bool
	key      *ecdsa.PrivateKey
	safeAddr common.Address
	ethCli   *ethclient.Client
	ctfABI   abi.ABI
	safeABI  abi.ABI
}

// New creates a Merger, initialising the Ethereum client and ABIs.
func New() *Merger {
	m := &Merger{}

	// Parse merge private key
	if config.MergePrivateKey == "" {
		log.Println("[merger] MERGE_PRIVATE_KEY not set — on-chain MERGE disabled")
		return m
	}
	key, err := clob.ParsePrivateKey(config.MergePrivateKey)
	if err != nil {
		log.Printf("[merger] invalid MERGE_PRIVATE_KEY: %v", err)
		return m
	}
	m.key = key

	if config.FunderAddress == "" {
		log.Println("[merger] FUNDER_ADDRESS not set — on-chain MERGE disabled")
		return m
	}
	m.safeAddr = common.HexToAddress(config.FunderAddress)

	// Connect to Polygon
	cli, err := ethclient.Dial(config.PolygonRPC)
	if err != nil {
		log.Printf("[merger] failed to connect to %s: %v", config.PolygonRPC, err)
		return m
	}
	m.ethCli = cli

	// Parse ABIs
	ctfABI, err := abi.JSON(strings.NewReader(conditionalTokensABI))
	if err != nil {
		log.Printf("[merger] ABI parse error: %v", err)
		return m
	}
	safeABI, err := abi.JSON(strings.NewReader(gnosisSafeABI))
	if err != nil {
		log.Printf("[merger] Safe ABI parse error: %v", err)
		return m
	}
	m.ctfABI = ctfABI
	m.safeABI = safeABI
	m.ready = true

	log.Printf("[merger] ready | Safe=%s... | signer=%s...",
		m.safeAddr.Hex()[:10], clob.AddressFromKey(key).Hex()[:10])
	return m
}

// Ready returns true if the merger is configured and connected.
func (m *Merger) Ready() bool {
	return m.ready
}

// Merge calls mergePositions on the ConditionalTokens contract via the Gnosis Safe.
// Returns the number of USDC units merged (≈ pairs count).
func (m *Merger) Merge(conditionID string, pairs float64) float64 {
	if !m.ready {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Convert conditionID hex → bytes32
	condBytes, err := hexToBytes32(conditionID)
	if err != nil {
		log.Printf("[merger] invalid conditionID %q: %v", conditionID, err)
		return 0
	}

	// Cap to on-chain balance
	onChainPairs := m.getOnChainPairs(ctx, condBytes)
	if onChainPairs < pairs {
		log.Printf("[merger] on-chain pairs (%.4f) < inventory (%.4f) — using on-chain", onChainPairs, pairs)
		pairs = onChainPairs
	}
	if pairs < 0.001 {
		log.Printf("[merger] on-chain balance too low to merge")
		return 0
	}

	amount := new(big.Int).SetInt64(int64(pairs * 1e6)) // 6 decimals

	// Build mergePositions calldata
	calldata, err := m.ctfABI.Pack("mergePositions",
		usdcAddr,
		[32]byte{}, // parentCollectionId = 0x0
		condBytes,
		[]*big.Int{big.NewInt(1), big.NewInt(2)}, // partition [UP, DOWN]
		amount,
	)
	if err != nil {
		log.Printf("[merger] pack mergePositions: %v", err)
		return 0
	}

	// Execute via Safe
	if err := m.execViaSafe(ctx, conditionalTokensAddr, calldata); err != nil {
		log.Printf("[merger] execViaSafe failed: %v", err)
		return 0
	}

	log.Printf("[merger] ✅ MERGE %.4f pairs → +$%.4f USDC | condition: %s...",
		pairs, pairs, conditionID[:8])
	return pairs
}

// ── Gnosis Safe execution ─────────────────────────────────────────────────

func (m *Merger) execViaSafe(ctx context.Context, to common.Address, data []byte) error {
	// Get Safe nonce
	nonceCalldata, _ := m.safeABI.Pack("nonce")
	result, err := m.ethCli.CallContract(ctx, ethereum.CallMsg{
		To:   &m.safeAddr,
		Data: nonceCalldata,
	}, nil)
	if err != nil {
		return fmt.Errorf("get safe nonce: %w", err)
	}
	var nonce *big.Int
	if len(result) >= 32 {
		nonce = new(big.Int).SetBytes(result[:32])
	} else {
		nonce = big.NewInt(0)
	}

	// Get tx hash from Safe
	zero := big.NewInt(0)
	zeroAddr := common.Address{}
	hashCalldata, err := m.safeABI.Pack("getTransactionHash",
		to, zero, data,
		uint8(0), // operation: CALL
		zero, zero, zero, zeroAddr, zeroAddr,
		nonce,
	)
	if err != nil {
		return fmt.Errorf("pack getTransactionHash: %w", err)
	}

	hashResult, err := m.ethCli.CallContract(ctx, ethereum.CallMsg{
		To:   &m.safeAddr,
		Data: hashCalldata,
	}, nil)
	if err != nil {
		return fmt.Errorf("getTransactionHash call: %w", err)
	}
	if len(hashResult) < 32 {
		return fmt.Errorf("unexpected hash result length: %d", len(hashResult))
	}
	var txHashBytes [32]byte
	copy(txHashBytes[:], hashResult[:32])

	// Sign the hash with the EOA key
	sig, err := crypto.Sign(txHashBytes[:], m.key)
	if err != nil {
		return fmt.Errorf("sign safe tx: %w", err)
	}
	// Safe expects v = 27/28, not 0/1
	sig[64] += 27

	// Build execTransaction calldata
	execCalldata, err := m.safeABI.Pack("execTransaction",
		to, zero, data,
		uint8(0), // CALL
		zero, zero, zero, zeroAddr, zeroAddr,
		sig,
	)
	if err != nil {
		return fmt.Errorf("pack execTransaction: %w", err)
	}

	// Get signer address and nonce
	signerAddr := clob.AddressFromKey(m.key)
	signerNonce, err := m.ethCli.PendingNonceAt(ctx, signerAddr)
	if err != nil {
		return fmt.Errorf("get signer nonce: %w", err)
	}

	// Gas estimation
	gasLimit, err := m.ethCli.EstimateGas(ctx, ethereum.CallMsg{
		From: signerAddr,
		To:   &m.safeAddr,
		Data: execCalldata,
	})
	if err != nil {
		log.Printf("[merger] gas estimate failed, using 500000: %v", err)
		gasLimit = 500000
	}
	gasLimit = gasLimit * 12 / 10 // +20% buffer

	// Gas price
	gasPrice, err := m.ethCli.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("gas price: %w", err)
	}

	// Build and sign the Ethereum transaction
	chainID := big.NewInt(137) // Polygon
	tx := types.NewTransaction(signerNonce, m.safeAddr, zero, gasLimit, gasPrice, execCalldata)
	signer := types.NewEIP155Signer(chainID)
	signedTx, err := types.SignTx(tx, signer, m.key)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	// Broadcast
	if err := m.ethCli.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}
	log.Printf("[merger] tx broadcast: %s", signedTx.Hash().Hex())

	// Wait for receipt
	return m.waitForReceipt(ctx, signedTx.Hash())
}

func (m *Merger) waitForReceipt(ctx context.Context, txHash common.Hash) error {
	for {
		receipt, err := m.ethCli.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.Status == 1 {
				log.Printf("[merger] tx confirmed in block %d", receipt.BlockNumber)
				return nil
			}
			return fmt.Errorf("tx reverted in block %d", receipt.BlockNumber)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// ── On-chain balance check ────────────────────────────────────────────────

// getOnChainPairs returns the minimum of UP and DOWN token balances
// held by the Safe, capped to the inventory estimate.
func (m *Merger) getOnChainPairs(ctx context.Context, condBytes [32]byte) float64 {
	// Token IDs are computed as keccak256(conditionId + outcomeIndex)
	// UP = index 0, DOWN = index 1 (for binary markets)
	upTokenID := positionID(condBytes, 0)
	downTokenID := positionID(condBytes, 1)

	upBal := m.tokenBalance(ctx, upTokenID)
	downBal := m.tokenBalance(ctx, downTokenID)

	up := float64(upBal.Int64()) / 1e6
	down := float64(downBal.Int64()) / 1e6

	if up < down {
		return up
	}
	return down
}

func (m *Merger) tokenBalance(ctx context.Context, tokenID *big.Int) *big.Int {
	calldata, _ := m.ctfABI.Pack("balanceOf", m.safeAddr, tokenID)
	result, err := m.ethCli.CallContract(ctx, ethereum.CallMsg{
		To:   &conditionalTokensAddr,
		Data: calldata,
	}, nil)
	if err != nil || len(result) < 32 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(result[:32])
}

// positionID computes the ERC-1155 token ID for a given condition + outcome index.
// positionId = keccak256(keccak256(parentCollectionId | conditionId | indexSet))
// For binary markets: UP=indexSet=1 (binary 01), DOWN=indexSet=2 (binary 10)
func positionID(conditionID [32]byte, outcomeIndex int) *big.Int {
	// indexSet: UP = 0b01 = 1, DOWN = 0b10 = 2
	indexSet := big.NewInt(int64(1 << outcomeIndex))

	// collectionId = keccak256(abi.encodePacked(parentCollectionId, conditionId, indexSet))
	parentColl := [32]byte{} // zero bytes
	indexSetBytes := make([]byte, 32)
	indexSet.FillBytes(indexSetBytes)

	collectionIDInput := append(parentColl[:], conditionID[:]...)
	collectionIDInput = append(collectionIDInput, indexSetBytes...)
	collectionID := crypto.Keccak256(collectionIDInput)

	// positionId = keccak256(abi.encodePacked(collateralToken, collectionId))
	tokenAddrBytes := usdcAddr.Bytes()
	posInput := append(tokenAddrBytes, collectionID...)
	posIDBytes := crypto.Keccak256(posInput)

	return new(big.Int).SetBytes(posIDBytes)
}

// ── Helpers ───────────────────────────────────────────────────────────────

func hexToBytes32(hexStr string) ([32]byte, error) {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	if len(b) > 32 {
		return [32]byte{}, fmt.Errorf("hex too long: %d bytes", len(b))
	}
	copy(out[32-len(b):], b)
	return out, nil
}

// IsResolved checks if a condition has been resolved on-chain.
// (Placeholder — full implementation checks PayoutDenominator > 0)
func (m *Merger) IsResolved(conditionID string) bool {
	// TODO: call ConditionalTokens.payoutDenominator(conditionId)
	// For now return false (assume unresolved)
	return false
}
