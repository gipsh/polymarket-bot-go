// Package pricer fetches token prices from the Polymarket CLOB REST API.
package pricer

// Prices holds the current bid/ask midpoint for both sides of a binary market.
type Prices struct {
	Up     float64 // UP / YES token price  (0–1)
	Down   float64 // DOWN / NO token price (0–1)
	Spread float64 // Up + Down (ideally close to 1.0)
}

// RESTFetcher fetches prices via the CLOB REST API.
// Used as fallback when the WebSocket cache is stale.
type RESTFetcher struct {
	// TODO: http client, CLOB host
}

// NewRESTFetcher creates a fetcher pointing at the given CLOB host.
func NewRESTFetcher(clobHost string) *RESTFetcher {
	panic("not implemented")
}

// GetPrices fetches the current midpoint prices for the given token IDs.
func (f *RESTFetcher) GetPrices(upTokenID, downTokenID string) (Prices, error) {
	panic("not implemented")
}
