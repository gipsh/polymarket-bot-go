// Package onchain handles on-chain operations via go-ethereum.
//
// All calls go through the Gnosis Safe (FUNDER_ADDRESS) via execTransaction,
// because the Safe holds the conditional tokens and USDC — not the EOA directly.
//
// Python reference: merger.py, setup_safe_allowances.py
package onchain

import "github.com/ethereum/go-ethereum/ethclient"

const (
	// ConditionalTokens contract on Polygon mainnet
	ConditionalTokensAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

	// USDC.e collateral on Polygon
	USDCAddress = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"

	// Polymarket exchange contracts (need USDC.approve from Safe)
	CTFExchangeAddress          = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	NegRiskCTFExchangeAddress   = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
	NegRiskAdapterAddress       = "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296"
)

// Safe wraps a Gnosis Safe 1.3.0 and provides execTransaction signing.
type Safe struct {
	// TODO: go-ethereum client, safe address, owner private key
}

// NewSafe creates a Safe wrapper.
//   rpcURL       — Polygon RPC endpoint
//   safeAddress  — Gnosis Safe contract address (FUNDER_ADDRESS)
//   ownerKey     — MetaMask EOA private key (hex, controls the Safe)
func NewSafe(rpcURL, safeAddress, ownerKey string) (*Safe, error) {
	panic("not implemented")
}

// ExecTransaction signs and submits a call through the Safe.
// Returns the tx hash.
func (s *Safe) ExecTransaction(to string, data []byte, label string) (string, error) {
	panic("not implemented")
}

// Merger executes on-chain MERGE via ConditionalTokens.mergePositions()
// through the Gnosis Safe's execTransaction.
type Merger struct {
	safe   *Safe
	client *ethclient.Client
}

// NewMerger creates a Merger.
func NewMerger(safe *Safe) (*Merger, error) {
	panic("not implemented")
}

// GetOnChainPairs returns the actual mergeable pairs available on-chain.
// Reads from ConditionalTokens.balanceOf() — source of truth.
func (m *Merger) GetOnChainPairs(conditionID string) (float64, error) {
	panic("not implemented")
}

// IsResolved returns true if the condition has been resolved on-chain.
// (payoutDenominator > 0)
func (m *Merger) IsResolved(conditionID string) (bool, error) {
	panic("not implemented")
}

// Merge calls mergePositions via the Safe for the given condition.
// Caps pairs to the actual on-chain balance to prevent reverts.
// Returns USDC recovered (= pairs merged).
func (m *Merger) Merge(conditionID string, pairs float64) (float64, error) {
	panic("not implemented")
}
