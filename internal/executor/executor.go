// Package executor places market orders and triggers MERGE operations.
// Mirror of Python executor.py.
package executor

import (
	"fmt"
	"log"
	"sync"
	"time"

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

// BuyLimit places a GTC limit order and polls until filled, cancelled, or timeout.
// Returns OrderResult with actual fill price for slippage check.
func (e *Executor) BuyLimit(
	conditionID, upTokenID, downTokenID, side string,
	usdcAmount, limitPrice float64,
) types.OrderResult {
	tokenID := upTokenID
	if side == "DOWN" {
		tokenID = downTokenID
	}
	tokenSize := usdcAmount / limitPrice

	if e.dryRun {
		log.Printf("[executor] [DRY_RUN] LIMIT BUY %s | $%.2f @ %.4f | size=%.3f tokens | token: %s...",
			side, usdcAmount, limitPrice, tokenSize, tokenID[:12])
		e.inv.RecordBuy(conditionID, upTokenID, downTokenID, side, tokenSize, usdcAmount)
		return types.OrderResult{
			Success:        true,
			TokenID:        tokenID,
			Side:           side,
			USDCSpent:      usdcAmount,
			TokensReceived: tokenSize,
			OrderID:        "dry-run-limit",
		}
	}

	resp, err := e.client.PlaceLimitOrder(clob.LimitOrderRequest{
		ConditionID: conditionID,
		UpTokenID:   upTokenID,
		DownTokenID: downTokenID,
		Side:        side,
		Price:       limitPrice,
		Size:        tokenSize,
	})
	if err != nil {
		log.Printf("[executor] LIMIT order failed (%s $%.2f @ %.3f): %v", side, usdcAmount, limitPrice, err)
		return types.OrderResult{Success: false, TokenID: tokenID, Side: side, Error: err.Error()}
	}

	orderID := getString(resp, "orderID")
	status := getString(resp, "status")
	log.Printf("[executor] LIMIT BUY %s placed | $%.2f @ %.4f | order: %s | status: %s",
		side, usdcAmount, limitPrice, orderID, status)

	// If already matched immediately
	if status == "matched" {
		tokensReceived := getFloat(resp, "takingAmount")
		if tokensReceived == 0 {
			tokensReceived = tokenSize
		}
		e.inv.RecordBuy(conditionID, upTokenID, downTokenID, side, tokensReceived, usdcAmount)
		e.checkSlippage(side, limitPrice, usdcAmount/tokensReceived)
		return types.OrderResult{
			Success:        true,
			TokenID:        tokenID,
			Side:           side,
			USDCSpent:      usdcAmount,
			TokensReceived: tokensReceived,
			OrderID:        orderID,
		}
	}

	// Poll until filled or timeout
	deadline := time.Now().Add(time.Duration(config.ARBLimitTimeoutSecs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		st, sizeFilled, err := e.client.GetOrderStatus(orderID)
		if err != nil {
			log.Printf("[executor] poll order %s: %v", orderID[:12], err)
			continue
		}
		if st == "matched" || (st == "live" && sizeFilled > 0) {
			tokensReceived := sizeFilled
			if tokensReceived == 0 {
				tokensReceived = tokenSize
			}
			actualPrice := usdcAmount / tokensReceived
			e.inv.RecordBuy(conditionID, upTokenID, downTokenID, side, tokensReceived, usdcAmount)
			e.checkSlippage(side, limitPrice, actualPrice)
			log.Printf("[executor] ✅ LIMIT BUY %s filled | %.3f tokens @ %.4f", side, tokensReceived, actualPrice)
			return types.OrderResult{
				Success:        true,
				TokenID:        tokenID,
				Side:           side,
				USDCSpent:      usdcAmount,
				TokensReceived: tokensReceived,
				OrderID:        orderID,
			}
		}
		if st == "cancelled" {
			break
		}
		log.Printf("[executor] LIMIT BUY %s waiting... (status=%s, filled=%.3f)", side, st, sizeFilled)
	}

	// Timeout — cancel order
	log.Printf("[executor] LIMIT BUY %s timeout (%ds) — cancelling %s...",
		side, config.ARBLimitTimeoutSecs, orderID[:12])
	if err := e.client.CancelOrder(orderID); err != nil {
		log.Printf("[executor] cancel failed: %v", err)
	} else {
		log.Printf("[executor] LIMIT BUY %s cancelled | order: %s", side, orderID[:12])
	}
	return types.OrderResult{
		Success: false,
		TokenID: tokenID,
		Side:    side,
		Error:   fmt.Sprintf("Limit order not filled within %ds — cancelled", config.ARBLimitTimeoutSecs),
		OrderID: orderID,
	}
}

// checkSlippage logs a warning if actual fill price exceeds the limit price by more than threshold.
func (e *Executor) checkSlippage(side string, expectedPrice, actualPrice float64) {
	if expectedPrice <= 0 {
		return
	}
	slippagePct := (actualPrice - expectedPrice) / expectedPrice * 100
	if slippagePct > config.ARBSlippageMaxPct {
		log.Printf("[executor] ⚠️  HIGH SLIPPAGE %s: expected=%.4f actual=%.4f slippage=%.1f%% (threshold=%.0f%%) — book may be thin",
			side, expectedPrice, actualPrice, slippagePct, config.ARBSlippageMaxPct)
	}
}

// BuyArbBoth concurrently buys both UP and DOWN sides using GTC limit orders.
// This is the ARB both-sides strategy.
func (e *Executor) BuyArbBoth(
	conditionID, upTokenID, downTokenID string,
	upUSDC, downUSDC, upPrice, downPrice float64,
) (upResult, downResult types.OrderResult) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if config.ARBUseLimitOrders {
			upResult = e.BuyLimit(conditionID, upTokenID, downTokenID, "UP", upUSDC, upPrice)
		} else {
			upResult = e.BuyMarket(conditionID, upTokenID, downTokenID, "UP", upUSDC, upPrice)
		}
	}()

	go func() {
		defer wg.Done()
		if config.ARBUseLimitOrders {
			downResult = e.BuyLimit(conditionID, upTokenID, downTokenID, "DOWN", downUSDC, downPrice)
		} else {
			downResult = e.BuyMarket(conditionID, upTokenID, downTokenID, "DOWN", downUSDC, downPrice)
		}
	}()

	wg.Wait()
	return
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
