// Package executor places market orders and coordinates MERGE operations.
//
// Python reference: executor.py
package executor

import (
	"github.com/gipsh/polymarket-bot-go/internal/clob"
	"github.com/gipsh/polymarket-bot-go/internal/inventory"
	"github.com/gipsh/polymarket-bot-go/internal/onchain"
	"github.com/gipsh/polymarket-bot-go/internal/ws"
)

// OrderResult is returned by BuyMarket.
type OrderResult struct {
	Success        bool
	Side           string
	USDCSpent      float64
	TokensReceived float64
	OrderID        string
	Error          error
}

// Executor places orders and manages MERGE operations.
type Executor struct {
	client    *clob.Client
	inventory *inventory.Inventory
	merger    *onchain.Merger
	wsUser    *ws.WSUser
	dryRun    bool
}

// New creates an Executor.
func New(client *clob.Client, inv *inventory.Inventory, merger *onchain.Merger, dryRun bool) *Executor {
	panic("not implemented")
}

// BuyMarket places a FOK market order for the given side.
//   conditionID  — market identifier (for inventory tracking)
//   upTokenID    — UP outcome token
//   downTokenID  — DOWN outcome token
//   side         — "UP" or "DOWN"
//   usdcAmount   — how much USDC to spend
func (e *Executor) BuyMarket(conditionID, upTokenID, downTokenID, side string, usdcAmount float64) OrderResult {
	panic("not implemented")
}

// MergePairs merges all available YES/NO pairs for the given market.
// Reconciles inventory from API first, then caps to on-chain balance.
// Returns USDC recovered.
func (e *Executor) MergePairs(conditionID string) float64 {
	panic("not implemented")
}

// HandleFill is called by WSUser on each confirmed fill.
// Updates inventory with actual tokens received.
func (e *Executor) HandleFill(fill ws.FillEvent) {
	panic("not implemented")
}
