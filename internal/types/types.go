// Package types defines all shared domain types for the bot.
// Mirror of Python: pricer.Prices, market_finder.Market, fsm.State/Action, executor.OrderResult.
package types

import (
	"fmt"
	"time"
)

// ── Market ────────────────────────────────────────────────────────────────

// Market represents a single Up/Down hourly market on Polymarket.
type Market struct {
	Asset       string    // e.g. "BTC"
	Slug        string    // e.g. "bitcoin-up-or-down-february-22-9pm-et"
	ConditionID string    // hex condition ID (0x...)
	UpTokenID   string    // CLOB token ID for the UP outcome
	DownTokenID string    // CLOB token ID for the DOWN outcome
	EndDate     time.Time // market close time (UTC)
	Title       string    // human-readable title
}

// MinutesToClose returns minutes until the market resolves.
func (m *Market) MinutesToClose() float64 {
	return time.Until(m.EndDate).Minutes()
}

// IsOpen returns true if the market has not yet resolved.
func (m *Market) IsOpen() bool {
	return m.MinutesToClose() > 0
}

// IsClosingSoon returns true if the market closes within maxAgeH hours.
func (m *Market) IsClosingSoon(maxAgeH int) bool {
	mins := m.MinutesToClose()
	return mins > 0 && mins < float64(maxAgeH)*60
}

// String returns a human-readable summary.
func (m *Market) String() string {
	return fmt.Sprintf("Market(%s | %s | closes in %.0fm)", m.Asset, m.Title, m.MinutesToClose())
}

// ── Prices ────────────────────────────────────────────────────────────────

// MarketState classifies the current market condition.
type MarketState string

const (
	StateARB          MarketState = "ARB"
	StateGrey         MarketState = "GREY"
	StateMomentumUp   MarketState = "MOMENTUM_UP"
	StateMomentumDown MarketState = "MOMENTUM_DOWN"
	StateResolved     MarketState = "RESOLVED"
)

// Prices holds the current UP/DOWN prices and derived market state.
type Prices struct {
	Up     float64
	Down   float64
	Spread float64     // Up + Down
	State  MarketState
}

// Winner returns "UP" or "DOWN" depending on which price is higher.
func (p *Prices) Winner() string {
	if p.Up >= p.Down {
		return "UP"
	}
	return "DOWN"
}

// WinnerPrice returns the higher of the two prices.
func (p *Prices) WinnerPrice() float64 {
	if p.Up >= p.Down {
		return p.Up
	}
	return p.Down
}

// LoserPrice returns the lower of the two prices.
func (p *Prices) LoserPrice() float64 {
	if p.Up <= p.Down {
		return p.Up
	}
	return p.Down
}

// ClassifyPrices determines the MarketState from raw up/down prices.
func ClassifyPrices(up, down, arbThreshold, momentumTrigger float64) MarketState {
	winner := max64(up, down)
	spread := up + down
	if winner >= 0.99 {
		return StateResolved
	}
	if winner > momentumTrigger {
		if up > down {
			return StateMomentumUp
		}
		return StateMomentumDown
	}
	if spread < arbThreshold {
		return StateARB
	}
	return StateGrey
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ── FSM State / Action ───────────────────────────────────────────────────

// BotState is the FSM state for a given market.
type BotState int

const (
	BotIdle         BotState = iota
	BotARB          BotState = iota
	BotGrey         BotState = iota
	BotMomentumUp   BotState = iota
	BotMomentumDown BotState = iota
	BotResolution   BotState = iota
)

func (s BotState) String() string {
	switch s {
	case BotIdle:
		return "IDLE"
	case BotARB:
		return "ARB"
	case BotGrey:
		return "GREY"
	case BotMomentumUp:
		return "MOMENTUM_UP"
	case BotMomentumDown:
		return "MOMENTUM_DOWN"
	case BotResolution:
		return "RESOLUTION"
	default:
		return "UNKNOWN"
	}
}

// ActionKind represents what the bot should do.
type ActionKind string

const (
	ActionWait         ActionKind = "wait"
	ActionSkip         ActionKind = "skip"
	ActionBuyArb       ActionKind = "buy_arb"
	ActionBuyMomentum  ActionKind = "buy_momentum"
	ActionMerge        ActionKind = "merge"
	ActionBuyArbBoth   ActionKind = "buy_arb_both"
)

// Action is the decision the FSM returns for a given market.
type Action struct {
	Kind      ActionKind
	Side      string  // "UP" or "DOWN" for buy_arb
	MainSide  string  // for buy_momentum
	HedgeSide string  // for buy_momentum
	MainUSDC  float64
	HedgeUSDC float64
	ArbUSDC   float64
	Reason    string
}

// WaitAction creates a wait action with a reason.
func WaitAction(reason string) Action {
	return Action{Kind: ActionWait, Reason: reason}
}

// SkipAction creates a skip action with a reason.
func SkipAction(reason string) Action {
	return Action{Kind: ActionSkip, Reason: reason}
}

// BuyArbAction creates an arb buy action.
func BuyArbAction(side string, usdc float64, reason string) Action {
	return Action{Kind: ActionBuyArb, Side: side, ArbUSDC: usdc, Reason: reason}
}

// BuyMomentumAction creates a momentum buy action.
func BuyMomentumAction(main, hedge string, mainUSDC, hedgeUSDC float64, reason string) Action {
	return Action{
		Kind:      ActionBuyMomentum,
		MainSide:  main,
		HedgeSide: hedge,
		MainUSDC:  mainUSDC,
		HedgeUSDC: hedgeUSDC,
		Reason:    reason,
	}
}

// MergeAction creates a merge action.
func MergeAction(reason string) Action {
	return Action{Kind: ActionMerge, Reason: reason}
}

// BuyArbBothAction creates an action to buy both UP and DOWN sides in ARB mode.
func BuyArbBothAction(upUSDC, downUSDC float64, reason string) Action {
	return Action{
		Kind:      ActionBuyArbBoth,
		MainSide:  "UP",
		HedgeSide: "DOWN",
		MainUSDC:  upUSDC,
		HedgeUSDC: downUSDC,
		ArbUSDC:   upUSDC + downUSDC,
		Reason:    reason,
	}
}

// ── Order types ───────────────────────────────────────────────────────────

// OrderSide is the side of an order (BUY).
type OrderSide int

const (
	SideBuy  OrderSide = 0
	SideSell OrderSide = 1
)

// SignatureType mirrors Polymarket signature types.
type SignatureType int

const (
	SigEOA        SignatureType = 0
	SigPolyProxy  SignatureType = 1
	SigGnosisSafe SignatureType = 2
)

// CLOBOrder represents an order ready for submission to the Polymarket CLOB.
type CLOBOrder struct {
	Salt          string `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	Taker         string `json:"taker"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Nonce         string `json:"nonce"`
	FeeRateBps    string `json:"feeRateBps"`
	Side          int    `json:"side"`
	SignatureType int    `json:"signatureType"`
	Signature     string `json:"signature"`
}

// OrderResult holds the outcome of an order placement attempt.
type OrderResult struct {
	Success        bool
	TokenID        string
	Side           string // "UP" or "DOWN"
	USDCSpent      float64
	TokensReceived float64
	OrderID        string
	LimitOrderID   string // set when a GTC limit order is placed (for cancellation)
	Error          string
}

// ── API credentials ───────────────────────────────────────────────────────

// APICreds holds the Level-2 API credentials derived from the wallet.
type APICreds struct {
	APIKey     string
	APISecret  string
	Passphrase string
}

// ── Fill event ────────────────────────────────────────────────────────────

// FillEvent is emitted by the user WebSocket when an order is matched.
type FillEvent struct {
	OrderID string
	Side    string
	Size    float64
	Price   float64
	Outcome string
	TxHash  string
}
