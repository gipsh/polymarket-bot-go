// Package market fetches active Polymarket binary markets from the Gamma API.
package market

import "time"

// Market represents a single active binary (Up/Down) market.
type Market struct {
	Slug        string
	Asset       string // e.g. "BTC"
	ConditionID string // hex, used for ConditionalTokens
	UpTokenID   string // YES outcome token ID
	DownTokenID string // NO outcome token ID
	CloseTime   time.Time
}

// MinutesToClose returns how many minutes until this market closes.
func (m *Market) MinutesToClose() float64 {
	return time.Until(m.CloseTime).Minutes()
}

// Finder fetches active markets from the Gamma API.
type Finder struct {
	// TODO: http client, base URL, asset filter, look-ahead window
}

// NewFinder creates a Finder for the given asset slugs (e.g. ["bitcoin"]).
func NewFinder(assets []string, lookAheadMins int) *Finder {
	panic("not implemented")
}

// GetActiveMarkets returns binary markets closing within lookAheadMins.
// Markets are sorted by close time (nearest first).
func (f *Finder) GetActiveMarkets() ([]Market, error) {
	panic("not implemented")
}
