// Package config loads bot configuration from environment / .env file.
// Mirror of Python config.py.
package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// ── Polymarket API endpoints ─────────────────────────────────────────────
const (
	CLOBHost  = "https://clob.polymarket.com"
	GammaHost = "https://gamma-api.polymarket.com"
	ChainID   = 137 // Polygon mainnet
)

// ── Config fields (populated by Load) ───────────────────────────────────
var (
	// Credentials
	PrivateKey      string
	FunderAddress   string
	SignatureType   int    // 0=EOA, 1=Proxy, 2=GnosisSafe
	DryRun          bool
	LogLevel        string
	PolygonRPC      string
	MergePrivateKey string

	// Assets to trade (e.g. ["bitcoin"])
	Assets []string

	// FSM thresholds
	ARBThreshold      float64
	GreyZoneLow       float64
	MomentumTrigger   float64
	MomentumMaxEntry  float64

	// Order sizing (USDC)
	ARBOrderUSDC      float64
	ARBMaxUSDC        float64
	MomentumMainUSDC  float64
	MomentumHedgeUSDC float64
	MomentumMaxUSDC   float64

	// Timing
	PollIntervalSec  float64
	MarketRefreshMin int
	MaxMarketAgeH    int

	// Inventory
	InventoryFile string
)

// Load reads .env (if present) then overrides from OS env vars.
func Load() {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] No .env file found, using OS environment")
	}

	// Credentials
	PrivateKey      = getEnv("PRIVATE_KEY", "")
	FunderAddress   = getEnv("FUNDER_ADDRESS", "")
	SignatureType   = getEnvInt("SIGNATURE_TYPE", 0)
	DryRun          = getEnvBool("DRY_RUN", false)
	LogLevel        = getEnv("LOG_LEVEL", "INFO")
	PolygonRPC      = getEnv("POLYGON_RPC", "https://polygon-bor-rpc.publicnode.com")
	MergePrivateKey = getEnv("MERGE_PRIVATE_KEY", PrivateKey)

	// Assets
	assetsEnv := getEnv("ASSETS", "bitcoin")
	Assets = []string{}
	for _, a := range strings.Split(assetsEnv, ",") {
		if a = strings.TrimSpace(a); a != "" {
			Assets = append(Assets, a)
		}
	}

	// FSM thresholds
	ARBThreshold     = getEnvFloat("ARB_THRESHOLD", 0.97)
	GreyZoneLow      = getEnvFloat("GREY_ZONE_LOW", 0.75)
	MomentumTrigger  = getEnvFloat("MOMENTUM_TRIGGER", 0.85)
	MomentumMaxEntry = getEnvFloat("MOMENTUM_MAX_ENTRY", 0.92)

	// Order sizing
	ARBOrderUSDC      = getEnvFloat("ARB_ORDER_USDC", 5.0)
	ARBMaxUSDC        = getEnvFloat("ARB_MAX_USDC", 20.0)
	MomentumMainUSDC  = getEnvFloat("MOMENTUM_MAIN_USDC", 10.0)
	MomentumHedgeUSDC = getEnvFloat("MOMENTUM_HEDGE_USDC", 1.0)
	MomentumMaxUSDC   = getEnvFloat("MOMENTUM_MAX_USDC", 30.0)

	// Timing
	PollIntervalSec  = getEnvFloat("POLL_INTERVAL", 2.0)
	MarketRefreshMin = getEnvInt("MARKET_REFRESH_MIN", 10)
	MaxMarketAgeH    = getEnvInt("MAX_MARKET_AGE_H", 4)

	// Inventory
	InventoryFile = getEnv("INVENTORY_FILE", "inventory_state.json")
}

// ── Helpers ──────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		return strings.ToLower(v) == "true"
	}
	return fallback
}
