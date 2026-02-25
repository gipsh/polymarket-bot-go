// Package fsm implements the trading finite state machine.
// Mirror of Python fsm.py.
package fsm

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/inventory"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

// FSM determines the next action for a given market based on current prices.
// Mostly stateless; tracks per-market cooldowns and spending caps.
type FSM struct {
	mu             sync.Mutex
	lastMomentumTS map[string]time.Time
	momentumSpent  map[string]float64
	lastArbTS      map[string]time.Time
	arbSpent       map[string]float64
}

// New creates a new FSM instance.
func New() *FSM {
	return &FSM{
		lastMomentumTS: make(map[string]time.Time),
		momentumSpent:  make(map[string]float64),
		lastArbTS:      make(map[string]time.Time),
		arbSpent:       make(map[string]float64),
	}
}

const (
	momentumCooldown = 120 * time.Second // 2 min between momentum fills
	arbCooldown      = 5 * time.Second   // 5s between arb orders
)

// Step evaluates market conditions and returns (botState, action).
func (f *FSM) Step(
	conditionID string,
	prices *types.Prices,
	inv *inventory.Inventory,
	minutesToClose float64,
) (types.BotState, types.Action) {

	f.mu.Lock()
	defer f.mu.Unlock()

	// ── Market resolved or about to close: MERGE ──────────────────────
	if prices.State == types.StateResolved || minutesToClose < 1 {
		pairs := inv.GetMergeablePairs(conditionID)
		if pairs > 0.01 {
			return types.BotResolution, types.MergeAction(
				fmt.Sprintf("market closing (%.0fm) | %.2f pairs to merge", minutesToClose, pairs),
			)
		}
		return types.BotResolution, types.SkipAction("market resolved, no pairs to merge")
	}

	// ── MOMENTUM modes ────────────────────────────────────────────────
	if prices.State == types.StateMomentumUp || prices.State == types.StateMomentumDown {
		var mainSide, hedgeSide string
		var botState types.BotState
		if prices.State == types.StateMomentumUp {
			mainSide, hedgeSide, botState = "UP", "DOWN", types.BotMomentumUp
		} else {
			mainSide, hedgeSide, botState = "DOWN", "UP", types.BotMomentumDown
		}

		// Price ceiling: skip if already too expensive
		if prices.WinnerPrice() > config.MomentumMaxEntry {
			return botState, types.SkipAction(
				fmt.Sprintf("MOMENTUM price ceiling: %.3f > %.2f — too late to enter",
					prices.WinnerPrice(), config.MomentumMaxEntry),
			)
		}

		// Spending cap
		spent := f.momentumSpent[conditionID]
		if spent >= config.MomentumMaxUSDC {
			return botState, types.SkipAction(
				fmt.Sprintf("MOMENTUM cap reached ($%.0f/$%.0f) for %s...",
					spent, config.MomentumMaxUSDC, conditionID[:8]),
			)
		}

		// Cooldown
		if last, ok := f.lastMomentumTS[conditionID]; ok {
			if remaining := momentumCooldown - time.Since(last); remaining > 0 {
				return botState, types.WaitAction(
					fmt.Sprintf("MOMENTUM cooldown: %.0fs remaining", remaining.Seconds()),
				)
			}
		}

		// Build action
		remaining := config.MomentumMainUSDC
		if leftover := config.MomentumMaxUSDC - spent; leftover < remaining {
			remaining = leftover
		}
		f.lastMomentumTS[conditionID] = time.Now()
		f.momentumSpent[conditionID] = spent + remaining + config.MomentumHedgeUSDC

		fillNum := int(spent/config.MomentumMainUSDC) + 1
		return botState, types.BuyMomentumAction(
			mainSide, hedgeSide, remaining, config.MomentumHedgeUSDC,
			fmt.Sprintf("%s momentum: up=%.3f down=%.3f | fill #%d",
				mainSide, prices.Up, prices.Down, fillNum),
		)
	}

	// ── GREY zone: wait ────────────────────────────────────────────────
	if prices.State == types.StateGrey {
		return types.BotGrey, types.WaitAction(
			fmt.Sprintf("grey zone: spread=%.3f | winner=%.3f", prices.Spread, prices.WinnerPrice()),
		)
	}

	// ── ARBITRAGE ─────────────────────────────────────────────────────
	if prices.State == types.StateARB {
		// Spending cap
		arbSp := f.arbSpent[conditionID]
		if arbSp >= config.ARBMaxUSDC {
			return types.BotARB, types.SkipAction(
				fmt.Sprintf("ARB cap reached ($%.0f/$%.0f) for %s...",
					arbSp, config.ARBMaxUSDC, conditionID[:8]),
			)
		}

		// Cooldown
		if last, ok := f.lastArbTS[conditionID]; ok {
			if remaining := arbCooldown - time.Since(last); remaining > 0 {
				return types.BotARB, types.SkipAction(
					fmt.Sprintf("ARB cooldown: %.1fs", remaining.Seconds()),
				)
			}
		}

		// Both-sides ARB: buy UP + DOWN simultaneously
		reason := fmt.Sprintf("ARB both-sides: up=%.3f down=%.3f | spread=%.3f",
			prices.Up, prices.Down, prices.Spread)
		f.lastArbTS[conditionID] = time.Now()
		f.arbSpent[conditionID] = arbSp + config.ARBOrderUSDC*2
		return types.BotARB, types.BuyArbBothAction(config.ARBOrderUSDC, config.ARBOrderUSDC, reason)
	}

	// ── Fallback ───────────────────────────────────────────────────────
	log.Printf("[fsm] unknown state %s for %s...", prices.State, conditionID[:8])
	return types.BotIdle, types.SkipAction(fmt.Sprintf("unknown state: %s", prices.State))
}
