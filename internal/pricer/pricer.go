// Package pricer fetches real-time prices from the Polymarket CLOB REST API.
// UP and DOWN prices are fetched concurrently to halve latency.
// Mirror of Python pricer.py.
package pricer

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

const (
	priceEndpoint    = "/price"
	midpointEndpoint = "/midpoint"
)

// Pricer fetches prices from the Polymarket REST API.
type Pricer struct {
	host    string
	httpCli *http.Client
}

// NewPricer creates a REST-based pricer.
func NewPricer() *Pricer {
	return &Pricer{
		host: config.CLOBHost,
		httpCli: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

// GetPrices fetches UP and DOWN prices concurrently and returns classified Prices.
func (p *Pricer) GetPrices(upTokenID, downTokenID string) (*types.Prices, error) {
	var (
		upPrice, downPrice float64
		upErr, downErr     error
		wg                 sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		upPrice, upErr = p.fetchPrice(upTokenID)
	}()
	go func() {
		defer wg.Done()
		downPrice, downErr = p.fetchPrice(downTokenID)
	}()
	wg.Wait()

	if upErr != nil {
		log.Printf("[pricer] UP price error (%s...): %v", upTokenID[:12], upErr)
		upPrice = 0.5
	}
	if downErr != nil {
		log.Printf("[pricer] DOWN price error (%s...): %v", downTokenID[:12], downErr)
		downPrice = 0.5
	}

	state := types.ClassifyPrices(upPrice, downPrice, config.ARBThreshold, config.MomentumTrigger)
	return &types.Prices{
		Up:     upPrice,
		Down:   downPrice,
		Spread: upPrice + downPrice,
		State:  state,
	}, nil
}

// fetchPrice fetches the best ask price for a single token.
// Primary: GET /price?token_id=...&side=BUY
// Fallback: GET /midpoint?token_id=...
func (p *Pricer) fetchPrice(tokenID string) (float64, error) {
	// Primary: best ask
	if price, err := p.fetchBestAsk(tokenID); err == nil {
		return price, nil
	}

	// Fallback: midpoint
	return p.fetchMidpoint(tokenID)
}

func (p *Pricer) fetchBestAsk(tokenID string) (float64, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)
	params.Set("side", "BUY")

	resp, err := p.httpCli.Get(p.host + priceEndpoint + "?" + params.Encode())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Try {"price": "0.49"} or raw float string
	var pr struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &pr); err == nil && pr.Price != "" {
		return strconv.ParseFloat(pr.Price, 64)
	}
	if f, err := strconv.ParseFloat(string(body), 64); err == nil {
		return f, nil
	}
	return 0, fmt.Errorf("unparseable price: %s", body)
}

func (p *Pricer) fetchMidpoint(tokenID string) (float64, error) {
	resp, err := p.httpCli.Get(p.host + midpointEndpoint + "?token_id=" + tokenID)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Mid string `json:"mid"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(result.Mid, 64)
}
