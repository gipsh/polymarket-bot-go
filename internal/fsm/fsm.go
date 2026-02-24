// Package fsm implements the ARB/Momentum trading Finite State Machine.
//
// Python reference: fsm.py
//
// States:
//   IDLE        → no position, evaluating prices
//   ARB         → spread < threshold, buying both UP and DOWN
//   MOMENTUM    → one side > trigger, buying winner
//   RESOLUTION  → market closing soon, merging pairs
//   GREY        → waiting (spread ok but no signal)
//
// Transitions:
//   IDLE/GREY → ARB        when spread < ARB_THRESHOLD
//   IDLE/GREY → MOMENTUM   when winner > MOMENTUM_TRIGGER && price < MOMENTUM_MAX_ENTRY
//   any       → RESOLUTION when minutes_to_close < 5
//   ARB/MOM   → GREY       when position limits reached
package fsm

import "github.com/gipsh/polymarket-bot-go/internal/pricer"

// State is the current FSM state for a given market.
type State int

const (
	StateIdle       State = iota
	StateGrey
	StateARB
	StateMomentum
	StateResolution
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "IDLE"
	case StateGrey:
		return "GREY"
	case StateARB:
		return "ARB"
	case StateMomentum:
		return "MOMENTUM"
	case StateResolution:
		return "RESOLUTION"
	default:
		return "UNKNOWN"
	}
}

// ActionKind describes what the bot should do this tick.
type ActionKind string

const (
	ActionWait   ActionKind = "wait"
	ActionBuyARB ActionKind = "buy_arb"
	ActionBuyMom ActionKind = "buy_momentum"
	ActionMerge  ActionKind = "merge"
	ActionSkip   ActionKind = "skip"
)

// Action is the output of FSM.Step.
type Action struct {
	Kind   ActionKind
	Side   string // "UP" or "DOWN" (for buy actions)
	Reason string // human-readable explanation for logs
}

// FSMConfig holds the tuning parameters for the FSM.
type FSMConfig struct {
	ArbThreshold     float64 // default 0.97
	MomentumTrigger  float64 // default 0.85
	MomentumMaxEntry float64 // default 0.92 — don't buy if winner > this
	ResolutionMins   float64 // default 5.0 — switch to RESOLUTION mode
	ArbMaxUSDC       float64 // max USDC deployed per market in ARB mode
	MomentumMaxUSDC  float64 // max USDC deployed per market in MOMENTUM mode
}

// FSM evaluates market conditions and outputs a trading action each tick.
// It is stateless across markets (caller tracks per-market state if needed).
type FSM struct {
	cfg FSMConfig
}

// NewFSM creates a FSM with the given config.
func NewFSM(cfg FSMConfig) *FSM {
	return &FSM{cfg: cfg}
}

// Step evaluates the current market state and returns (state, action).
//
// Parameters:
//   conditionID   — identifies the market (for inventory lookup)
//   prices        — current UP/DOWN prices
//   investedUSDC  — how much USDC is already deployed in this market
//   minutesToClose — how many minutes until the market closes
func (f *FSM) Step(
	conditionID string,
	prices pricer.Prices,
	investedUSDC float64,
	minutesToClose float64,
) (State, Action) {
	panic("not implemented")
}
