// Package inventory tracks token holdings per market condition.
// Persists state to a JSON file so inventory survives restarts.
// Mirror of Python inventory.py.
package inventory

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/clob"
	"github.com/gipsh/polymarket-bot-go/internal/config"
)

const reconcileInterval = 120 * time.Second // max 1 reconcile per 2 minutes

// Entry holds the per-condition token state.
type Entry struct {
	UpTokenID     string  `json:"up_token_id"`
	DownTokenID   string  `json:"down_token_id"`
	UpBalance     float64 `json:"up_balance"`
	DownBalance   float64 `json:"down_balance"`
	TotalInvested float64 `json:"total_invested_usdc"`
	TotalMerged   float64 `json:"total_merged_usdc"`
}

// Inventory tracks all condition→token holdings.
type Inventory struct {
	mu            sync.Mutex
	filepath      string
	state         map[string]*Entry // conditionID → Entry
	lastReconcile time.Time
}

// New creates an inventory backed by the configured file.
func New() *Inventory {
	inv := &Inventory{
		filepath: config.InventoryFile,
		state:    make(map[string]*Entry),
	}
	inv.load()
	return inv
}

// ── Reads ─────────────────────────────────────────────────────────────────

// GetBalance returns the token balance for a side ("UP" or "DOWN").
func (inv *Inventory) GetBalance(conditionID, side string) float64 {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.state[conditionID]
	if !ok {
		return 0
	}
	if side == "UP" {
		return e.UpBalance
	}
	return e.DownBalance
}

// GetMergeablePairs returns the number of UP+DOWN pairs that can be merged.
func (inv *Inventory) GetMergeablePairs(conditionID string) float64 {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.state[conditionID]
	if !ok {
		return 0
	}
	return math.Min(e.UpBalance, e.DownBalance)
}

// GetImbalance returns (excessSide, excessAmount) to guide arb rebalancing.
func (inv *Inventory) GetImbalance(conditionID string) (string, float64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.state[conditionID]
	if !ok {
		return "DOWN", 0
	}
	if e.UpBalance >= e.DownBalance {
		return "DOWN", e.UpBalance - e.DownBalance
	}
	return "UP", e.DownBalance - e.UpBalance
}

// Summary returns a human-readable state string for a condition.
func (inv *Inventory) Summary(conditionID string) string {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.state[conditionID]
	if !ok {
		return fmt.Sprintf("[%s...] No inventory", conditionID[:8])
	}
	pairs := math.Min(e.UpBalance, e.DownBalance)
	return fmt.Sprintf("[%s...] UP=%.2f DOWN=%.2f | Pairs=%.2f | Invested=$%.2f | Merged=$%.2f",
		conditionID[:8], e.UpBalance, e.DownBalance, pairs, e.TotalInvested, e.TotalMerged)
}

// ── Writes ────────────────────────────────────────────────────────────────

// RecordBuy records a completed buy order.
func (inv *Inventory) RecordBuy(conditionID, upTokenID, downTokenID, side string, tokens, usdc float64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.ensure(conditionID, upTokenID, downTokenID)
	e := inv.state[conditionID]
	if side == "UP" {
		e.UpBalance += tokens
	} else {
		e.DownBalance += tokens
	}
	e.TotalInvested += usdc
	inv.save()
	log.Printf("[inventory] [%s...] +%.2f %s | UP=%.2f DOWN=%.2f",
		conditionID[:8], tokens, side, e.UpBalance, e.DownBalance)
}

// RecordMerge records a MERGE operation, removing matched pairs.
func (inv *Inventory) RecordMerge(conditionID string, pairs float64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.state[conditionID]
	if !ok {
		return
	}
	mergeable := math.Min(pairs, math.Min(e.UpBalance, e.DownBalance))
	e.UpBalance -= mergeable
	e.DownBalance -= mergeable
	e.TotalMerged += mergeable
	inv.save()
	log.Printf("[inventory] MERGE [%s...]: %.2f pairs → +$%.2f USDC",
		conditionID[:8], mergeable, mergeable)
}

// ── Reconcile from API ────────────────────────────────────────────────────

// ReconcileFromAPI rebuilds inventory from CLOB trade history.
// Rate-limited to once per reconcileInterval unless force=true.
func (inv *Inventory) ReconcileFromAPI(client *clob.Client, force bool) (int, error) {
	inv.mu.Lock()
	if !force && time.Since(inv.lastReconcile) < reconcileInterval {
		inv.mu.Unlock()
		return -1, nil // rate-limited
	}
	inv.lastReconcile = time.Now()
	inv.mu.Unlock()

	trades, err := client.GetTrades("")
	if err != nil {
		return 0, err
	}
	if len(trades) == 0 {
		log.Println("[inventory] reconcile: no trades — inventory unchanged")
		return 0, nil
	}

	// Rebuild per-condition balances from confirmed/matched BUY trades
	newState := make(map[string]*Entry)
	for _, t := range trades {
		if t.Market == "" {
			continue
		}
		if t.Status != "CONFIRMED" && t.Status != "MATCHED" {
			continue
		}
		if t.Side != "BUY" {
			continue
		}
		size := parseFloatStr(t.Size)
		if size == 0 {
			continue
		}
		if _, ok := newState[t.Market]; !ok {
			newState[t.Market] = &Entry{}
		}
		e := newState[t.Market]
		price := parseFloatStr(t.Price)
		if price == 0 {
			price = 1.0
		}
		e.TotalInvested += size * price

		switch t.Outcome {
		case "UP", "YES":
			e.UpBalance += size
			e.UpTokenID = t.AssetID
		case "DOWN", "NO":
			e.DownBalance += size
			e.DownTokenID = t.AssetID
		}
	}

	// Subtract already-merged amounts from existing state
	inv.mu.Lock()
	for cid, entry := range newState {
		if existing, ok := inv.state[cid]; ok {
			entry.TotalMerged = existing.TotalMerged
			entry.UpBalance = math.Max(0, entry.UpBalance-entry.TotalMerged)
			entry.DownBalance = math.Max(0, entry.DownBalance-entry.TotalMerged)
		}
	}
	inv.state = newState
	inv.save()
	inv.mu.Unlock()

	log.Printf("[inventory] reconciled: %d markets, %d trades", len(newState), len(trades))
	for cid, e := range newState {
		log.Printf("  [%s...] UP=%.2f DOWN=%.2f pairs=%.2f",
			cid[:8], e.UpBalance, e.DownBalance, math.Min(e.UpBalance, e.DownBalance))
	}
	return len(newState), nil
}

// ── Persistence ───────────────────────────────────────────────────────────

func (inv *Inventory) ensure(conditionID, upTokenID, downTokenID string) {
	if _, ok := inv.state[conditionID]; !ok {
		inv.state[conditionID] = &Entry{
			UpTokenID:   upTokenID,
			DownTokenID: downTokenID,
		}
	}
}

func (inv *Inventory) load() {
	if _, err := os.Stat(inv.filepath); os.IsNotExist(err) {
		return
	}
	data, err := os.ReadFile(inv.filepath)
	if err != nil {
		log.Printf("[inventory] load error: %v — starting fresh", err)
		return
	}
	if err := json.Unmarshal(data, &inv.state); err != nil {
		log.Printf("[inventory] parse error: %v — starting fresh", err)
		inv.state = make(map[string]*Entry)
		return
	}
	log.Printf("[inventory] loaded: %d markets tracked", len(inv.state))
}

func (inv *Inventory) save() {
	data, err := json.MarshalIndent(inv.state, "", "  ")
	if err != nil {
		log.Printf("[inventory] marshal error: %v", err)
		return
	}
	if err := os.WriteFile(inv.filepath, data, 0600); err != nil {
		log.Printf("[inventory] save error: %v", err)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

func parseFloatStr(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
