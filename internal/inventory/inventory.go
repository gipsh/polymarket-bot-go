// Package inventory tracks YES/NO token balances per market.
// Persists state to a JSON file so inventory survives restarts.
//
// Python reference: inventory.py
package inventory

import "github.com/gipsh/polymarket-bot-go/internal/clob"

// Entry tracks token holdings for a single market condition.
type Entry struct {
	ConditionID      string  `json:"condition_id"`
	UpTokenID        string  `json:"up_token_id"`
	DownTokenID      string  `json:"down_token_id"`
	UpBalance        float64 `json:"up_balance"`
	DownBalance      float64 `json:"down_balance"`
	TotalInvestedUSDC float64 `json:"total_invested_usdc"`
	TotalMergedUSDC  float64 `json:"total_merged_usdc"`
}

// MergeablePairs returns the number of YES/NO pairs that can be MERGEd.
func (e *Entry) MergeablePairs() float64 {
	if e.UpBalance < e.DownBalance {
		return e.UpBalance
	}
	return e.DownBalance
}

// Inventory tracks positions across all active markets.
type Inventory struct {
	// TODO: entries map, file path, last-reconcile timestamp, mutex
}

// New loads inventory from filepath (creates empty if not found).
func New(filepath string) (*Inventory, error) {
	panic("not implemented")
}

// RecordBuy updates balances after a confirmed fill.
func (inv *Inventory) RecordBuy(conditionID, upTokenID, downTokenID, side string, tokens, usdcSpent float64) {
	panic("not implemented")
}

// RecordMerge removes merged pairs from the inventory.
func (inv *Inventory) RecordMerge(conditionID string, pairs float64) {
	panic("not implemented")
}

// GetEntry returns the inventory entry for a market (nil if not tracked).
func (inv *Inventory) GetEntry(conditionID string) *Entry {
	panic("not implemented")
}

// TotalInvested returns total USDC deployed in a market.
func (inv *Inventory) TotalInvested(conditionID string) float64 {
	panic("not implemented")
}

// ReconcileFromAPI rebuilds balances from CLOB trade history.
// Rate-limited: skipped if called within 120s of last reconcile (unless force=true).
func (inv *Inventory) ReconcileFromAPI(client *clob.Client, force bool) error {
	panic("not implemented")
}
