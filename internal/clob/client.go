// Package clob wraps the Polymarket CLOB REST API.
//
// Implementation is adapted from:
// github.com/gipsh/go-polymarket-tools/convergence-bot/polymarket
//
// That package already implements:
//   - EIP-712 auth signature (ClobAuthDomain)
//   - HMAC L2 request headers
//   - API key derivation / creation
//   - Order creation via github.com/polymarket/go-order-utils
//   - GTC limit order posting, cancellation
//   - WebSocket user feed auth
//
// This package extends it with:
//   - FOK market order support  (TODO)
//   - GetTrades() for inventory reconcile  (TODO)
//   - GetBalanceAllowance()  (TODO)
package clob

// OrderSide represents the order direction.
type OrderSide string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"
)

// OrderType controls fill-or-kill vs good-till-cancelled behaviour.
type OrderType string

const (
	FOK OrderType = "FOK" // Fill-or-Kill: market order, must fill immediately
	GTC OrderType = "GTC" // Good-Till-Cancelled: limit order
)

// MarketOrderArgs is used for FOK market orders (our primary order type).
type MarketOrderArgs struct {
	TokenID string
	Amount  float64 // USDC to spend
	Side    OrderSide
}

// Trade represents a historical fill from the CLOB /data/trades endpoint.
type Trade struct {
	ID         string
	Market     string // condition_id
	AssetID    string // token_id
	Side       OrderSide
	Outcome    string // "Up" or "Down"
	Size       float64
	Price      float64
	Status     string
	MatchTime  int64
}

// BalanceAllowance holds USDC balance and per-contract allowances.
type BalanceAllowance struct {
	Balance    string            // raw USDC units (6 decimals)
	Allowances map[string]string // spender → allowance
}

// Client is the CLOB API client.
// TODO: embed/adapt from go-polymarket-tools
type Client struct {
	// TODO
}

// NewClient creates a new authenticated CLOB client.
// TODO: adapt Config from go-polymarket-tools
func NewClient( /* cfg Config */ ) (*Client, error) {
	panic("not implemented")
}

// PostMarketOrder places a FOK market order and returns the filled amount.
// TODO: implement — convergence-bot only has GTC limit orders
func (c *Client) PostMarketOrder(args MarketOrderArgs) (tokensReceived float64, err error) {
	panic("not implemented")
}

// GetTrades returns the full trade history for the authenticated account.
// Used by inventory.ReconcileFromAPI().
func (c *Client) GetTrades() ([]Trade, error) {
	panic("not implemented")
}

// GetBalanceAllowance returns USDC balance and exchange allowances.
func (c *Client) GetBalanceAllowance(sigType int) (*BalanceAllowance, error) {
	panic("not implemented")
}
