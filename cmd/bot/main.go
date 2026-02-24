// Polymarket ARB/Momentum bot — Go port.
//
// Architecture:
//   main goroutine  — market cycle (poll → FSM → execute)
//   goroutine       — WSPricer (price feed, updates cache)
//   goroutine       — WSUser   (fill feed, updates inventory)
//
// Python reference: bot.py
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	// TODO: import other packages once implemented
)

func main() {
	cfg := config.Load()
	log.Printf("🐾 polymarket-bot-go starting | dry_run=%v", cfg.DryRun)

	// TODO Phase 1: init market finder + REST pricer
	// TODO Phase 2: init CLOB client (adapted from go-polymarket-tools)
	// TODO Phase 3: init FSM + inventory + executor
	// TODO Phase 4: start WSPricer + WSUser goroutines
	// TODO Phase 5: init Merger (Gnosis Safe)
	// TODO Phase 6: main loop

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("skeleton only — nothing to run yet")
	<-quit
	log.Println("shutting down")
}
