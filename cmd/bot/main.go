// cmd/bot — Polymarket ARB/Momentum trading bot (Go rewrite).
//
// Usage:
//
//	./bot              # live trading
//	./bot --dry-run    # simulate, no real orders
//
// Environment: configure via .env file (same as Python version).
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/clob"
	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/executor"
	"github.com/gipsh/polymarket-bot-go/internal/fsm"
	"github.com/gipsh/polymarket-bot-go/internal/inventory"
	"github.com/gipsh/polymarket-bot-go/internal/market"
	"github.com/gipsh/polymarket-bot-go/internal/pricer"
	"github.com/gipsh/polymarket-bot-go/internal/types"
	"github.com/gipsh/polymarket-bot-go/internal/ws"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "Simulate without placing real orders")
	flag.Parse()

	// ── Load config ────────────────────────────────────────────────────
	config.Load()
	if *dryRun {
		config.DryRun = true
	}

	setupLogging()

	if config.DryRun {
		log.Println("============================================================")
		log.Println("  DRY RUN MODE — No real orders will be placed")
		log.Println("============================================================")
	}

	// ── Init components ────────────────────────────────────────────────
	clobClient, err := clob.NewClient()
	if err != nil {
		log.Fatalf("CLOB client init: %v", err)
	}

	inv := inventory.New()

	exec := executor.New(inv, clobClient, config.DryRun)
	fsmEngine := fsm.New()
	marketFinder := market.NewFinder()
	restPricer := pricer.NewPricer()
	wsPricer := ws.NewWSPricer()

	// ── Authenticate ───────────────────────────────────────────────────
	var wsUser *ws.UserClient
	if !config.DryRun && config.PrivateKey != "" {
		creds, err := clobClient.CreateOrDeriveAPICreds()
		if err != nil {
			log.Printf("[main] WARNING: failed to derive API creds: %v", err)
		} else {
			log.Printf("[main] API credentials derived ✓")
			clobClient.SetAPICreds(creds)

			// Reconcile inventory from trade history
			if n, err := inv.ReconcileFromAPI(clobClient, true); err != nil {
				log.Printf("[main] startup reconcile failed: %v", err)
			} else if n > 0 {
				log.Printf("[main] startup reconcile: %d markets", n)
			}

			// User WebSocket (fill feed)
			wsUser = ws.NewUserClient(creds, exec.HandleFill)
			wsUser.Start()
		}
	}

	// ── Start WS price feed ────────────────────────────────────────────
	wsPricer.Start()
	defer wsPricer.Stop()
	if wsUser != nil {
		defer wsUser.Stop()
	}

	// ── Graceful shutdown ──────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sigCh
		log.Printf("[main] received signal %s — shutting down", s)
		os.Exit(0)
	}()

	// ── Main loop ──────────────────────────────────────────────────────
	log.Println("🐾 Polymarket Bot (Go) starting up...")
	log.Printf("[main] Assets: %v | Interval: %.1fs", config.Assets, config.PollIntervalSec)

	var (
		markets          []*types.Market
		lastMarketRefresh time.Time
		lastLogState     string
		lastLogTS        time.Time
	)

	pollInterval := time.Duration(config.PollIntervalSec * float64(time.Second))

	for {
		// Refresh market list every MarketRefreshMin minutes
		if time.Since(lastMarketRefresh) >= time.Duration(config.MarketRefreshMin)*time.Minute || markets == nil {
			log.Println("[main] refreshing market list...")
			newMarkets, err := marketFinder.GetActiveMarkets()
			if err != nil {
				log.Printf("[main] market refresh error: %v", err)
			} else {
				markets = newMarkets
				lastMarketRefresh = time.Now()
				for _, m := range markets {
					log.Printf("[main]  → %s", m)
					wsPricer.Subscribe([]string{m.UpTokenID, m.DownTokenID})
					if wsUser != nil {
						wsUser.Subscribe(m.ConditionID)
					}
				}
			}
		}

		// Process each market
		for _, m := range markets {
			// Get prices: prefer fresh WS data, fall back to REST
			var prices *types.Prices
			wsFresh := wsPricer.IsFresh(m.UpTokenID, 4*time.Second) &&
				wsPricer.IsFresh(m.DownTokenID, 4*time.Second)

			if wsFresh {
				prices = wsPricer.GetPrices(m.UpTokenID, m.DownTokenID)
			} else {
				p, err := restPricer.GetPrices(m.UpTokenID, m.DownTokenID)
				if err != nil {
					log.Printf("[main] REST price error for %s: %v", m.Asset, err)
					continue
				}
				prices = p
				// Seed WS cache with REST data
				wsPricer.UpdateCache(m.UpTokenID, prices.Up)
				wsPricer.UpdateCache(m.DownTokenID, prices.Down)
			}

			// Run FSM
			state, action := fsmEngine.Step(m.ConditionID, prices, inv, m.MinutesToClose())

			// Adaptive poll interval
			pollInterval = adaptInterval(prices)

			// Logging: state change, trade action, or 30s heartbeat
			now := time.Now()
			stateKey := state.String()
			shouldLog := action.Kind != types.ActionWait && action.Kind != types.ActionSkip ||
				stateKey != lastLogState ||
				now.Sub(lastLogTS) >= 30*time.Second

			if shouldLog {
				slot := extractSlot(m.Slug)
				log.Printf("%s %s-ET [%s] UP=%.3f DOWN=%.3f spread=%.3f closes=%.0fm | %s: %s",
					m.Asset, slot, stateKey,
					prices.Up, prices.Down, prices.Spread, m.MinutesToClose(),
					action.Kind, action.Reason,
				)
				lastLogState = stateKey
				lastLogTS = now
			}

			// Execute action
			executeAction(m, action, prices, exec)

			log.Printf("[inventory] %s", inv.Summary(m.ConditionID))
		}

		if len(markets) == 0 {
			log.Println("[main] no active markets — waiting...")
		}

		time.Sleep(pollInterval)
	}
}

// ── Action execution ──────────────────────────────────────────────────────

func executeAction(m *types.Market, action types.Action, prices *types.Prices, exec *executor.Executor) {
	switch action.Kind {
	case types.ActionWait, types.ActionSkip:
		return

	case types.ActionBuyArb:
		priceHint := prices.Up
		if action.Side == "DOWN" {
			priceHint = prices.Down
		}
		result := exec.BuyMarket(
			m.ConditionID, m.UpTokenID, m.DownTokenID,
			action.Side, action.ArbUSDC, priceHint,
		)
		if result.Success {
			log.Printf("  ✓ ARB BUY %s | $%.2f → %.3f tokens", action.Side, result.USDCSpent, result.TokensReceived)
		} else {
			log.Printf("  ✗ ARB BUY failed: %s", result.Error)
		}

	case types.ActionBuyMomentum:
		mainPrice := prices.Up
		if action.MainSide == "DOWN" {
			mainPrice = prices.Down
		}
		hedgePrice := prices.Down
		if action.HedgeSide == "DOWN" {
			hedgePrice = prices.Down
		}

		mainResult := exec.BuyMarket(
			m.ConditionID, m.UpTokenID, m.DownTokenID,
			action.MainSide, action.MainUSDC, mainPrice,
		)
		if mainResult.Success {
			log.Printf("  ✓ MOMENTUM %s (main) | $%.2f → %.3f tokens",
				action.MainSide, mainResult.USDCSpent, mainResult.TokensReceived)
		}

		hedgeResult := exec.BuyMarket(
			m.ConditionID, m.UpTokenID, m.DownTokenID,
			action.HedgeSide, action.HedgeUSDC, hedgePrice,
		)
		if hedgeResult.Success {
			log.Printf("  ✓ MOMENTUM %s (hedge) | $%.2f → %.3f tokens",
				action.HedgeSide, hedgeResult.USDCSpent, hedgeResult.TokensReceived)
		}

	case types.ActionMerge:
		pairs := exec.MergePairs(m.ConditionID)
		if pairs > 0 {
			log.Printf("  ✓ MERGE %.2f pairs → +$%.2f USDC", pairs, pairs)
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

func adaptInterval(prices *types.Prices) time.Duration {
	switch prices.State {
	case types.StateMomentumUp, types.StateMomentumDown:
		return 300 * time.Millisecond
	case types.StateARB:
		return 500 * time.Millisecond
	case types.StateResolved:
		return 5 * time.Second
	}
	if prices.Spread < 0.975 {
		return 500 * time.Millisecond
	}
	if prices.Spread < 0.985 {
		return time.Second
	}
	return time.Duration(config.PollIntervalSec * float64(time.Second))
}

func extractSlot(slug string) string {
	// "bitcoin-up-or-down-february-22-9pm-et" → "9pm"
	parts := splitN(slug, "-", -1)
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return "?"
}

func splitN(s, sep string, n int) []string {
	result := []string{}
	for {
		i := lastIndex(s, sep)
		if i < 0 || (n > 0 && len(result) >= n-1) {
			result = append([]string{s}, result...)
			break
		}
		result = append([]string{s[i+len(sep):]}, result...)
		s = s[:i]
	}
	return result
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func setupLogging() {
	log.SetFlags(log.Ldate | log.Ltime)
	// If LOG_LEVEL=DEBUG, could set more verbose output here
}
