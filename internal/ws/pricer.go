// Package ws provides WebSocket clients for Polymarket price and fill feeds.
package ws

import (
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/pricer"
)

// PriceUpdate is sent on the price channel when a new price arrives.
type PriceUpdate struct {
	TokenID   string
	Price     float64
	Timestamp time.Time
}

// WSPricer subscribes to the Polymarket market WebSocket and maintains
// an in-memory price cache. Goroutine-safe.
//
// Python reference: ws_pricer.py
type WSPricer struct {
	// TODO: cache map[tokenID]priceEntry, mutex, WS conn, reconnect loop
}

// NewWSPricer creates a WSPricer (not yet connected).
func NewWSPricer(wsURL string) *WSPricer {
	panic("not implemented")
}

// Start connects and begins the read loop in a goroutine.
func (p *WSPricer) Start() error {
	panic("not implemented")
}

// Stop closes the connection and stops the read loop.
func (p *WSPricer) Stop() {
	panic("not implemented")
}

// Subscribe adds token IDs to the active subscription.
func (p *WSPricer) Subscribe(tokenIDs ...string) error {
	panic("not implemented")
}

// GetPrices returns the cached prices for a market's token pair.
// Returns (prices, fresh) where fresh=false if the cache is stale.
func (p *WSPricer) GetPrices(upTokenID, downTokenID string, maxAgeS float64) (pricer.Prices, bool) {
	panic("not implemented")
}

// IsFresh reports whether the cache entry for tokenID is within maxAgeS seconds.
func (p *WSPricer) IsFresh(tokenID string, maxAgeS float64) bool {
	panic("not implemented")
}
