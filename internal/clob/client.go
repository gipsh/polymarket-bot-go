// Package clob provides a Polymarket CLOB API client.
//
// Authentication:
//   - Level 1 (L1): personal_sign of timestamp → used for /auth/api-key
//   - Level 2 (L2): HMAC-SHA256 → used for order management
//
// Order signing uses EIP-712 (see eip712.go).
package clob

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

// Client is the Polymarket CLOB HTTP client.
type Client struct {
	host      string
	key       *ecdsa.PrivateKey
	address   common.Address
	funder    common.Address   // Gnosis Safe or EOA funder
	sigType   types.SignatureType
	creds     *types.APICreds
	httpCli   *http.Client
}

// NewClient creates a new CLOB client from the global config.
func NewClient() (*Client, error) {
	var key *ecdsa.PrivateKey
	var addr common.Address

	if config.PrivateKey != "" {
		var err error
		key, err = ParsePrivateKey(config.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid PRIVATE_KEY: %w", err)
		}
		addr = AddressFromKey(key)
	}

	funder := common.HexToAddress(config.FunderAddress)

	return &Client{
		host:    config.CLOBHost,
		key:     key,
		address: addr,
		funder:  funder,
		sigType: types.SignatureType(config.SignatureType),
		httpCli: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ── Authentication ────────────────────────────────────────────────────────

// CreateOrDeriveAPICreds derives L2 API credentials by signing with the private key.
// This calls POST /auth/api-key with L1 auth headers.
func (c *Client) CreateOrDeriveAPICreds() (*types.APICreds, error) {
	if c.key == nil {
		return nil, fmt.Errorf("no private key configured")
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig, err := PersonalSign(ts, c.key)
	if err != nil {
		return nil, fmt.Errorf("L1 sign: %w", err)
	}

	req, err := http.NewRequest("GET", c.host+"/auth/api-key", nil)
	if err != nil {
		return nil, err
	}
	c.addL1Headers(req, sig, ts, "0")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /auth/api-key: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("/auth/api-key: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		APIKey     string `json:"apiKey"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse /auth/api-key: %w", err)
	}

	creds := &types.APICreds{
		APIKey:     result.APIKey,
		APISecret:  result.Secret,
		Passphrase: result.Passphrase,
	}
	c.creds = creds
	return creds, nil
}

// SetAPICreds sets pre-derived L2 credentials.
func (c *Client) SetAPICreds(creds *types.APICreds) {
	c.creds = creds
}

// ── Price fetching ────────────────────────────────────────────────────────

// PriceResponse from GET /price.
type PriceResponse struct {
	Price string `json:"price"`
}

// GetPrice fetches the best ask price for a single token (BUY side).
func (c *Client) GetPrice(tokenID string) (float64, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)
	params.Set("side", "BUY")

	resp, err := c.httpCli.Get(c.host + "/price?" + params.Encode())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var pr PriceResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		// may be a raw float string
		if f, e := strconv.ParseFloat(strings.TrimSpace(string(body)), 64); e == nil {
			return f, nil
		}
		return 0, fmt.Errorf("parse price: %w", err)
	}
	return strconv.ParseFloat(pr.Price, 64)
}

// GetMidpoint fetches the midpoint price for a single token.
func (c *Client) GetMidpoint(tokenID string) (float64, error) {
	resp, err := c.httpCli.Get(c.host + "/midpoint?token_id=" + tokenID)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Mid string `json:"mid"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(result.Mid, 64)
}

// ── Order placement ───────────────────────────────────────────────────────

// MarketOrderRequest defines the parameters for a market (FOK) order.
type MarketOrderRequest struct {
	ConditionID string
	UpTokenID   string
	DownTokenID string
	Side        string  // "UP" or "DOWN"
	USDCAmount  float64
	PriceHint   float64 // best known price for token estimation
}

// PlaceMarketOrder builds, signs, and submits a market (FOK) BUY order.
// Returns the full response from the CLOB or an error.
func (c *Client) PlaceMarketOrder(req MarketOrderRequest) (map[string]interface{}, error) {
	if c.key == nil {
		return nil, fmt.Errorf("no private key — cannot place orders")
	}
	if c.creds == nil {
		return nil, fmt.Errorf("API creds not set — call CreateOrDeriveAPICreds first")
	}

	tokenID := req.UpTokenID
	if req.Side == "DOWN" {
		tokenID = req.DownTokenID
	}

	// Build order params
	salt := big.NewInt(rand.Int63())
	makerAmt := USDCToUnits(req.USDCAmount)

	// Estimate taker amount from price hint (tokens = USDC / price)
	var takerAmt *big.Int
	if req.PriceHint > 0 {
		estimated := req.USDCAmount / req.PriceHint
		takerAmt = USDCToUnits(estimated)
	} else {
		// 0.5 default
		takerAmt = USDCToUnits(req.USDCAmount * 2)
	}

	tokenIDBig, err := TokenIDFromHex(tokenID)
	if err != nil {
		return nil, fmt.Errorf("invalid tokenID: %w", err)
	}

	maker := c.address
	if c.sigType == types.SigGnosisSafe {
		maker = c.funder // Safe is the maker; EOA is the signer
	}

	params := OrderParams{
		Salt:          salt,
		Maker:         maker,
		Signer:        c.address,
		Taker:         common.Address{}, // zero = open order
		TokenID:       tokenIDBig,
		MakerAmount:   makerAmt,
		TakerAmount:   takerAmt,
		Expiration:    big.NewInt(0),
		Nonce:         big.NewInt(0),
		FeeRateBps:    big.NewInt(0),
		Side:          0, // BUY
		SignatureType: uint8(c.sigType),
	}

	sig, err := BuildAndSignOrder(params, c.key, false)
	if err != nil {
		return nil, fmt.Errorf("sign order: %w", err)
	}

	order := map[string]interface{}{
		"salt":          salt.String(),
		"maker":         strings.ToLower(maker.Hex()),
		"signer":        strings.ToLower(c.address.Hex()),
		"taker":         "0x0000000000000000000000000000000000000000",
		"tokenId":       tokenIDBig.String(),
		"makerAmount":   makerAmt.String(),
		"takerAmount":   takerAmt.String(),
		"expiration":    "0",
		"nonce":         "0",
		"feeRateBps":    "0",
		"side":          0,
		"signatureType": int(c.sigType),
		"signature":     sig,
	}

	body := map[string]interface{}{
		"order":     order,
		"orderType": "FOK",
	}

	return c.postL2("/order", body)
}

// ── Trade history ─────────────────────────────────────────────────────────

// Trade represents a single trade entry from /data/trades.
type Trade struct {
	Market    string `json:"market"`
	Side      string `json:"side"`
	Outcome   string `json:"outcome"`
	Size      string `json:"size"`
	Price     string `json:"price"`
	Status    string `json:"status"`
	AssetID   string `json:"asset_id"`
	Timestamp string `json:"timestamp"`
}

// GetTrades fetches recent trade history (L2 auth required).
func (c *Client) GetTrades(nextCursor string) ([]Trade, error) {
	if c.creds == nil {
		return nil, fmt.Errorf("API creds not set")
	}

	path := "/data/trades"
	if nextCursor != "" && nextCursor != "MA==" {
		path += "?next_cursor=" + nextCursor
	}

	body, err := c.getL2(path)
	if err != nil {
		return nil, err
	}

	// Response can be []Trade or {data: []Trade}
	var trades []Trade
	if err := json.Unmarshal(body, &trades); err == nil {
		return trades, nil
	}
	var wrapped struct {
		Data []Trade `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("parse /data/trades: %w", err)
	}
	return wrapped.Data, nil
}

// ── L1 / L2 helpers ──────────────────────────────────────────────────────

func (c *Client) addL1Headers(req *http.Request, sig, ts, nonce string) {
	addr := strings.ToLower(c.address.Hex())
	if c.sigType == types.SigGnosisSafe {
		addr = strings.ToLower(c.funder.Hex())
	}
	req.Header.Set("POLY_ADDRESS", addr)
	req.Header.Set("POLY_SIGNATURE", sig)
	req.Header.Set("POLY_TIMESTAMP", ts)
	req.Header.Set("POLY_NONCE", nonce)
}

// hmacL2Sign computes the HMAC-SHA256 L2 signature.
// message = timestamp + method + path + body
func (c *Client) hmacL2Sign(ts, method, path, body string) string {
	secret, _ := base64.URLEncoding.DecodeString(c.creds.APISecret)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + method + path + body))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *Client) addL2Headers(req *http.Request, method, path, body string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := c.hmacL2Sign(ts, method, path, body)
	req.Header.Set("POLY_ADDRESS", strings.ToLower(c.address.Hex()))
	req.Header.Set("POLY_SIGNATURE", sig)
	req.Header.Set("POLY_TIMESTAMP", ts)
	req.Header.Set("POLY_PASSPHRASE", c.creds.Passphrase)
	req.Header.Set("POLY_API_KEY", c.creds.APIKey)
	req.Header.Set("Content-Type", "application/json")
}

func (c *Client) postL2(path string, payload interface{}) (map[string]interface{}, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.host+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	c.addL2Headers(req, "POST", path, string(bodyBytes))

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func (c *Client) getL2(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.host+path, nil)
	if err != nil {
		return nil, err
	}
	c.addL2Headers(req, "GET", path, "")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}
