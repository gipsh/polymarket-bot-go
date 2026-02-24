package ws

import "time"

// FillEvent is fired when one of our orders gets filled.
type FillEvent struct {
	OrderID string
	Side    string // "UP" or "DOWN"
	Price   float64
	Tokens  float64
	At      time.Time
}

// FillHandler is called on each confirmed fill.
type FillHandler func(FillEvent)

// WSUser subscribes to the authenticated Polymarket user WebSocket
// and fires FillHandler on each fill.
//
// Python reference: ws_user.py
// Already implemented in go-polymarket-tools/convergence-bot — port from there.
type WSUser struct {
	// TODO: embed/adapt from convergence-bot executor.go (StartFillListener)
}

// NewWSUser creates a WSUser (not yet connected).
// apiKey, apiSecret, apiPassphrase are the CLOB API credentials.
func NewWSUser(wsURL, apiKey, apiSecret, apiPassphrase string, onFill FillHandler) *WSUser {
	panic("not implemented")
}

// Start connects and begins the read loop in a goroutine.
func (u *WSUser) Start() error {
	panic("not implemented")
}

// Stop closes the connection gracefully.
func (u *WSUser) Stop() {
	panic("not implemented")
}

// TrackOrder registers an order ID so fills can be matched to a side.
func (u *WSUser) TrackOrder(orderID, side string) {
	panic("not implemented")
}
