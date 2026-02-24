package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Wallet
	PrivateKey    string
	FunderAddress string
	SignatureType int

	// CLOB
	CLOBHost string
	ChainID  int

	// Polygon RPC
	PolygonRPC string

	// Trading
	DryRun            bool
	ArbOrderUSDC      float64
	ArbThreshold      float64
	MomentumMainUSDC  float64
	MomentumHedgeUSDC float64
	MomentumMaxUSDC   float64
	MomentumTrigger   float64
	MomentumMaxEntry  float64

	// Bot
	Assets        []string // e.g. ["bitcoin"]
	PollIntervalS float64
	LookAheadMins int

	// Files
	InventoryFile string
}

// Load reads .env and returns a Config. Panics on missing required fields.
func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		PrivateKey:        mustEnv("PRIVATE_KEY"),
		FunderAddress:     mustEnv("FUNDER_ADDRESS"),
		SignatureType:     envInt("SIGNATURE_TYPE", 2),
		CLOBHost:          envStr("CLOB_HOST", "https://clob.polymarket.com"),
		ChainID:           envInt("CHAIN_ID", 137),
		PolygonRPC:        envStr("POLYGON_RPC", "https://polygon-bor-rpc.publicnode.com"),
		DryRun:            envBool("DRY_RUN", false),
		ArbOrderUSDC:      envFloat("ARB_ORDER_USDC", 5.0),
		ArbThreshold:      envFloat("ARB_THRESHOLD", 0.97),
		MomentumMainUSDC:  envFloat("MOMENTUM_MAIN_USDC", 10.0),
		MomentumHedgeUSDC: envFloat("MOMENTUM_HEDGE_USDC", 1.0),
		MomentumMaxUSDC:   envFloat("MOMENTUM_MAX_USDC", 30.0),
		MomentumTrigger:   envFloat("MOMENTUM_TRIGGER", 0.85),
		MomentumMaxEntry:  envFloat("MOMENTUM_MAX_ENTRY", 0.92),
		PollIntervalS:     envFloat("POLL_INTERVAL", 2.0),
		LookAheadMins:     envInt("LOOK_AHEAD_MINS", 240),
		InventoryFile:     envStr("INVENTORY_FILE", "inventory_state.json"),
		Assets:            []string{"bitcoin"}, // TODO: parse ASSETS env
	}

	return cfg
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("missing required env var: " + key)
	}
	return v
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
