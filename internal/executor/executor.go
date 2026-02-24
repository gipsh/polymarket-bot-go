// Package executor places market orders and triggers MERGE operations.
// Mirror of Python executor.py.
package executor

import (
	"fmt"
	"log"

	"github.com/gipsh/polymarket-bot-go/internal/clob"
	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/inventory"
	"github.com/gipsh/polymarket-bot-go/internal/merger"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

// Executor places orders and executes MERGE via the CLOB client.
type Executor struct {
	inv    *inventory.Inventory
	client *clob.Client
	merger *merger.Merger
	dryRun bool
}

// New creates an Executor. If dryRun=true, no real orders are placed.
func New(inv *inventory.Inventory, client *clob.Client, dryRun bool) *Executor {
	m := merger.New()
	return &Executor{
		inv:    inv,
		client: client,
		merger: m,
		dryRun: dryRun || config.DryRun,
	}
}

// BuyMarket places a market (FOK) BUY order for the given side.
func (e *Executor) BuyMarket(
	conditionID, upTokenID, downTokenID, side string,
	usdcAmount, priceHint float64,
) types.OrderResult {
	tokenID := upTokenID
	if side == "DOWN" {
		tokenID = downTokenID
	}

	if e.dryRun {
		estimated := usdcAmount / max64(priceHint, 0.01)
		log.Printf("[executor] [DRY_RUN] Would BUY %s | $%.2f USDC | token: %s...",
			side, usdcAmount, tokenID[:12])
		e.inv.RecordBuy(conditionID, upTokenID, downTokenID, side, estimated, usdcAmount)
		return types.OrderResult{
			Success:        true,
			TokenID:        tokenID,
			Side:           side,
			USDCSpent:      usdcAmount,
			TokensReceived: estimated,
			OrderID:        "dry-run",
		}
	}

	resp, err := e.client.PlaceMarketOrder(clob.MarketOrderRequest{
		ConditionID: conditionID,
		UpTokenID:   upTokenID,
		DownTokenID: downTokenID,
		Side:        side,
		USDCAmount:  usdcAmount,
		PriceHint:   priceHint,
	})
	if err != nil {
		log.Printf("[executor] Order failed (%s $%.2f): %v", side, usdcAmount, err)
		return types.OrderResult{
			Success: false,
			TokenID: tokenID,
			Side:    side,
			Error:   err.Error(),
		}
	}

	// Parse response: orderID, takingAmount (tokens), makingAmount (USDC)
	orderID := getString(resp, "orderID")
	tokensReceived := getFloat(resp, "takingAmount")
	usdcSpent := getFloat(resp, "makingAmount")
	if usdcSpent == 0 {
		usdcSpent = usdcAmount
	}
	if tokensReceived == 0 && usdcSpent > 0 {
		tokensReceived = usdcSpent / max64(priceHint, 0.01)
		log.Printf("[executor] takingAmount missing — estimating: $%.2f / %.3f = %.2f",
			usdcSpent, priceHint, tokensReceived)
	}

	log.Printf("[executor] BUY %s executed | $%.2f USDC → %.3f tokens | order: %s",
		side, usdcSpent, tokensReceived, orderID)
	e.inv.RecordBuy(conditionID, upTokenID, downTokenID, side, tokensReceived, usdcSpent)

	return types.OrderResult{
		Success:        true,
		TokenID:        tokenID,
		Side:           side,
		USDCSpent:      usdcSpent,
		TokensReceived: tokensReceived,
		OrderID:        orderID,
	}
}

// HandleFill is called by the user WebSocket when a fill is confirmed.
func (e *Executor) HandleFill(fill types.FillEvent) {
	log.Printf("[executor] ✅ WS fill | order=%s... %s %.4f @ %.4f outcome=%s tx=%s...",
		fill.OrderID[:16], fill.Side, fill.Size, fill.Price, fill.Outcome, fill.TxHash[:16])
}

// MergePairs executes on-chain MERGE for available UP+DOWN pairs.
// Returns the number of pairs merged (= USDC received).
func (e *Executor) MergePairs(conditionID string) float64 {
	// Pre-merge reconcile
	if _, err := e.inv.ReconcileFromAPI(e.client, false); err != nil {
		log.Printf("[executor] pre-merge reconcile failed: %v", err)
	}

	pairs := e.inv.GetMergeablePairs(conditionID)
	if pairs < 0.01 {
		log.Printf("[executor] no mergeable pairs for %s...", conditionID[:8])
		return 0
	}

	if e.dryRun {
		log.Printf("[executor] [DRY_RUN] Would MERGE %.2f pairs → +$%.2f USDC | market: %s...",
			pairs, pairs, conditionID[:8])
		e.inv.RecordMerge(conditionID, pairs)
		return pairs
	}

	if !e.merger.Ready() {
		log.Printf("[executor] on-chain MERGE unavailable. %.2f pairs for %s... — do manually on UI.",
			pairs, conditionID[:8])
		return 0
	}

	merged := e.merger.Merge(conditionID, pairs)
	if merged > 0 {
		e.inv.RecordMerge(conditionID, merged)
	}
	return merged
}

// ── Helpers ───────────────────────────────────────────────────────────────

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case string:
			var f float64
			fmt.Sscanf(n, "%f", &f)
			return f
		}
	}
	return 0
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
