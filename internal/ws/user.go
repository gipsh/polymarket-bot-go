// User WebSocket client — receives authenticated fill events.
// Mirror of Python ws_user.py.
package ws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gipsh/polymarket-bot-go/internal/types"
)

const userWSURL = "wss://ws-subscriptions-clob.polymarket.com/ws/user"

// OnFillFunc is called when a fill event arrives.
type OnFillFunc func(types.FillEvent)

// UserClient maintains an authenticated connection to the user channel.
type UserClient struct {
	apiKey     string
	apiSecret  string
	passphrase string
	onFill     OnFillFunc
	conn       *websocket.Conn
	running    bool
	stopCh     chan struct{}
}

// NewUserClient creates an authenticated user WebSocket client.
func NewUserClient(creds *types.APICreds, onFill OnFillFunc) *UserClient {
	return &UserClient{
		apiKey:     creds.APIKey,
		apiSecret:  creds.APISecret,
		passphrase: creds.Passphrase,
		onFill:     onFill,
		stopCh:     make(chan struct{}),
	}
}

// Start launches the background connection loop.
func (u *UserClient) Start() {
	u.running = true
	go u.connectForever()
	log.Println("[ws/user] started")
}

// Stop gracefully shuts down.
func (u *UserClient) Stop() {
	u.running = false
	close(u.stopCh)
	if u.conn != nil {
		_ = u.conn.Close()
	}
	log.Println("[ws/user] stopped")
}

// Subscribe subscribes to fill events for a given condition ID.
func (u *UserClient) Subscribe(conditionID string) {
	if u.conn == nil {
		return
	}
	msg := map[string]interface{}{
		"type":  "user",
		"markets": []string{conditionID},
	}
	data, _ := json.Marshal(msg)
	_ = u.conn.WriteMessage(websocket.TextMessage, data)
}

// ── Internal ──────────────────────────────────────────────────────────────

func (u *UserClient) connectForever() {
	for u.running {
		if err := u.listen(); err != nil && u.running {
			log.Printf("[ws/user] disconnected: %v — reconnecting in %s", err, reconnectDelay)
			time.Sleep(reconnectDelay)
		}
	}
}

func (u *UserClient) listen() error {
	// Build auth headers for WS connection
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := u.hmacSign(ts, "GET", "/ws/user", "")

	headers := map[string][]string{
		"POLY_ADDRESS":    {u.apiKey},
		"POLY_SIGNATURE":  {sig},
		"POLY_TIMESTAMP":  {ts},
		"POLY_PASSPHRASE": {u.passphrase},
	}

	conn, _, err := websocket.DefaultDialer.Dial(userWSURL, headers)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	u.conn = conn

	log.Println("[ws/user] connected to Polymarket user channel")

	// Ping loop
	stopPing := make(chan struct{})
	go func() {
		tick := time.NewTicker(pingInterval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				_ = conn.WriteMessage(websocket.TextMessage, []byte("PING"))
			case <-stopPing:
				return
			}
		}
	}()
	defer close(stopPing)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			u.conn = nil
			return err
		}
		if string(msg) == "PONG" {
			continue
		}
		u.handleMessage(msg)
	}
}

func (u *UserClient) handleMessage(raw []byte) {
	var events []json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		events = []json.RawMessage{raw}
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

		if etype == "trade" || etype == "fill" || etype == "TRADE" {
			u.handleFill(ev)
		}
	}
}

func (u *UserClient) handleFill(raw json.RawMessage) {
	var ev struct {
		OrderID  string  `json:"order_id"`
		Side     string  `json:"side"`
		Size     float64 `json:"size"`
		Price    float64 `json:"price"`
		Outcome  string  `json:"outcome"`
		TxHash   string  `json:"transaction_hash"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return
	}
	if u.onFill != nil {
		u.onFill(types.FillEvent{
			OrderID: ev.OrderID,
			Side:    ev.Side,
			Size:    ev.Size,
			Price:   ev.Price,
			Outcome: ev.Outcome,
			TxHash:  ev.TxHash,
		})
	}
}

func (u *UserClient) hmacSign(ts, method, path, body string) string {
	secret, _ := base64.URLEncoding.DecodeString(u.apiSecret)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + method + path + body))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}
