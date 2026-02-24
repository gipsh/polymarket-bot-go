// Package ws provides WebSocket clients for Polymarket real-time feeds.
// This file: market price feed (ws_pricer.py equivalent).
package ws

import (
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

const (
	marketWSURL    = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
	pingInterval   = 9 * time.Second
	reconnectDelay = 2 * time.Second
)

// PriceCache stores per-token prices and timestamps.
type priceEntry struct {
	price float64
	ts    time.Time
}

// Pricer maintains a live WebSocket connection to the Polymarket market feed
// and caches best-ask prices per token ID.
type Pricer struct {
	mu            sync.RWMutex
	cache         map[string]priceEntry
	subscribed    map[string]bool
	pendingSubs   []string
	conn          *websocket.Conn
	running       bool
	stopCh        chan struct{}
}

// NewWSPricer creates a new WebSocket-based price feed.
func NewWSPricer() *Pricer {
	return &Pricer{
		cache:      make(map[string]priceEntry),
		subscribed: make(map[string]bool),
		stopCh:     make(chan struct{}),
	}
}

// Subscribe registers token IDs for price updates.
func (p *Pricer) Subscribe(tokenIDs []string) {
	p.mu.Lock()
	var newIDs []string
	for _, id := range tokenIDs {
		if !p.subscribed[id] {
			p.subscribed[id] = true
			newIDs = append(newIDs, id)
		}
	}
	p.mu.Unlock()

	if len(newIDs) == 0 {
		return
	}
	// Queue for next send (will be sent when connected)
	p.mu.Lock()
	p.pendingSubs = append(p.pendingSubs, newIDs...)
	conn := p.conn
	p.mu.Unlock()

	if conn != nil {
		_ = p.sendSubscribe(conn, newIDs)
	}
}

// Start launches the background connection loop.
func (p *Pricer) Start() {
	p.running = true
	go p.connectForever()
	log.Println("[ws/pricer] started")
}

// Stop gracefully shuts down the WebSocket.
func (p *Pricer) Stop() {
	p.running = false
	close(p.stopCh)
	p.mu.Lock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.mu.Unlock()
	log.Println("[ws/pricer] stopped")
}

// GetPrices returns cached prices for the given token pair.
// Falls back to 0.5 if not yet received.
func (p *Pricer) GetPrices(upTokenID, downTokenID string) *types.Prices {
	p.mu.RLock()
	up   := p.getPrice(upTokenID)
	down := p.getPrice(downTokenID)
	p.mu.RUnlock()

	state := types.ClassifyPrices(up, down, config.ARBThreshold, config.MomentumTrigger)
	return &types.Prices{
		Up:     up,
		Down:   down,
		Spread: up + down,
		State:  state,
	}
}

// IsFresh returns true if the token has a recent price (within maxAge).
func (p *Pricer) IsFresh(tokenID string, maxAge time.Duration) bool {
	p.mu.RLock()
	e, ok := p.cache[tokenID]
	p.mu.RUnlock()
	return ok && time.Since(e.ts) < maxAge
}

// UpdateCache allows external seeding (e.g. REST fallback data).
func (p *Pricer) UpdateCache(tokenID string, price float64) {
	if price > 0 && price < 1 {
		p.mu.Lock()
		p.cache[tokenID] = priceEntry{price: price, ts: time.Now()}
		p.mu.Unlock()
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────

func (p *Pricer) getPrice(tokenID string) float64 {
	if e, ok := p.cache[tokenID]; ok {
		return e.price
	}
	return 0.5
}

func (p *Pricer) connectForever() {
	for p.running {
		if err := p.listen(); err != nil && p.running {
			log.Printf("[ws/pricer] disconnected: %v — reconnecting in %s", err, reconnectDelay)
			time.Sleep(reconnectDelay)
		}
	}
}

func (p *Pricer) listen() error {
	conn, _, err := websocket.DefaultDialer.Dial(marketWSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	p.mu.Lock()
	p.conn = conn
	pending := p.pendingSubs
	p.pendingSubs = nil
	p.mu.Unlock()

	log.Println("[ws/pricer] connected to Polymarket market channel")

	// Subscribe all registered tokens
	p.mu.RLock()
	allTokens := make([]string, 0, len(p.subscribed))
	for id := range p.subscribed {
		allTokens = append(allTokens, id)
	}
	p.mu.RUnlock()
	_ = p.sendSubscribe(conn, append(allTokens, pending...))

	// Ping goroutine
	stopPing := make(chan struct{})
	go func() {
		tick := time.NewTicker(pingInterval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if err := conn.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
					return
				}
			case <-stopPing:
				return
			}
		}
	}()
	defer close(stopPing)

	// Read loop
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			p.mu.Lock()
			p.conn = nil
			p.mu.Unlock()
			return err
		}
		if string(msg) == "PONG" {
			continue
		}
		p.handleMessage(msg)
	}
}

func (p *Pricer) sendSubscribe(conn *websocket.Conn, tokenIDs []string) error {
	if len(tokenIDs) == 0 {
		return nil
	}
	msg := map[string]interface{}{
		"assets_ids":             tokenIDs,
		"type":                   "market",
		"custom_feature_enabled": true,
	}
	data, _ := json.Marshal(msg)
	return conn.WriteMessage(websocket.TextMessage, data)
}

// ── Message handling ──────────────────────────────────────────────────────

func (p *Pricer) handleMessage(raw []byte) {
	var events []json.RawMessage

	// Could be a list or a single object
	if err := json.Unmarshal(raw, &events); err != nil {
		var single json.RawMessage = raw
		events = []json.RawMessage{single}
	}

	for _, ev := range events {
		var base struct {
			EventType string `json:"event_type"`
			Type      string `json:"type"`
		}
		_ = json.Unmarshal(ev, &base)
		etype := base.EventType
		if etype == "" {
			etype = base.Type
		}

		switch etype {
		case "book":
			p.handleBook(ev)
		case "price_change":
			p.handlePriceChange(ev)
		case "best_bid_ask":
			p.handleBestBidAsk(ev)
		case "last_trade_price":
			p.handleLastTrade(ev)
		}
	}
}

func (p *Pricer) handleBook(raw json.RawMessage) {
	var ev struct {
		AssetID string `json:"asset_id"`
		Asks    []struct {
			Price string `json:"price"`
		} `json:"asks"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.AssetID == "" || len(ev.Asks) == 0 {
		return
	}
	best := 1.0
	for _, a := range ev.Asks {
		if f, err := strconv.ParseFloat(a.Price, 64); err == nil && f > 0 {
			if f < best {
				best = f
			}
		}
	}
	p.UpdateCache(ev.AssetID, best)
}

func (p *Pricer) handlePriceChange(raw json.RawMessage) {
	var ev struct {
		AssetID string  `json:"asset_id"`
		Price   float64 `json:"price"`
		Side    string  `json:"side"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.AssetID == "" {
		return
	}
	if ev.Side == "" || ev.Side == "ASK" || ev.Side == "SELL" {
		current := 0.5
		p.mu.RLock()
		if e, ok := p.cache[ev.AssetID]; ok {
			current = e.price
		}
		p.mu.RUnlock()
		if ev.Price > 0 && abs64(ev.Price-current) < 0.15 {
			p.UpdateCache(ev.AssetID, ev.Price)
		}
	}
}

func (p *Pricer) handleBestBidAsk(raw json.RawMessage) {
	var ev struct {
		AssetID string  `json:"asset_id"`
		BestAsk float64 `json:"best_ask"`
		Ask     float64 `json:"ask"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.AssetID == "" {
		return
	}
	ask := ev.BestAsk
	if ask == 0 {
		ask = ev.Ask
	}
	p.UpdateCache(ev.AssetID, ask)
}

func (p *Pricer) handleLastTrade(raw json.RawMessage) {
	var ev struct {
		AssetID string  `json:"asset_id"`
		Price   float64 `json:"price"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.AssetID == "" {
		return
	}
	// Only use last trade if we have no fresh data
	if !p.IsFresh(ev.AssetID, 2*time.Second) {
		p.UpdateCache(ev.AssetID, ev.Price)
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
