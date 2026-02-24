# polymarket-bot-go

Go rewrite of the [Polymarket ARB/Momentum trading bot](https://github.com/gipsh/polymarket-bot).

## Why Go?

- **Goroutines** replace Python threads + asyncio — cleaner concurrency
- **Single binary** — no venv, no Python version issues, ships as one file
- **Type safety** — compile-time checks catch bugs the Python version found at runtime
- **Lower latency** — native code, no GIL, faster WebSocket and HTTP

## Architecture

```
cmd/bot/main.go         ← main loop (market discovery → price → FSM → execute)
internal/
  config/               ← loads .env (same vars as Python version)
  types/                ← shared domain types (Market, Prices, Action, BotState)
  clob/
    client.go           ← CLOB HTTP client (L1/L2 auth, order placement)
    eip712.go           ← EIP-712 order signing + personal_sign (no SDK needed)
  market/               ← MarketFinder: discovers hourly Up/Down markets via Gamma API
  pricer/               ← parallel REST pricer (UP + DOWN fetched concurrently)
  ws/
    pricer.go           ← WebSocket price feed (wss://ws-subscriptions-clob.polymarket.com)
    user.go             ← authenticated fill event feed
  fsm/                  ← Finite State Machine: GREY → ARB / MOMENTUM → MERGE
  inventory/            ← per-condition token tracking, persisted to JSON
  executor/             ← places market orders, triggers MERGE
  merger/               ← on-chain mergePositions via Gnosis Safe execTransaction
```

## Wallet Architecture

```
MetaMask EOA (PRIVATE_KEY / MERGE_PRIVATE_KEY)
    └── controls ──→ Gnosis Safe 1.3.0 (FUNDER_ADDRESS)
                          └── holds USDC
                          └── signs CLOB orders (SIGNATURE_TYPE=2)
                          └── executes mergePositions on-chain
```

## Setup

```bash
cp .env.example .env
# Fill in PRIVATE_KEY, FUNDER_ADDRESS, MERGE_PRIVATE_KEY
```

## Build & Run

```bash
# Install Go 1.23+
go build -o polymarket-bot ./cmd/bot

# Run live
./polymarket-bot

# Simulate (no real orders)
./polymarket-bot --dry-run

# Background with log
bash start.sh
bash stop.sh
```

## Strategy

| Mode      | Trigger                      | Action                                         |
|-----------|------------------------------|------------------------------------------------|
| ARB       | UP + DOWN < 0.97             | Buy cheaper side → MERGE when ≥ 1 pair         |
| MOMENTUM  | Winner > 0.85 and ≤ 0.92    | Buy winner (main) + loser (hedge $1 insurance) |
| MERGE     | Market closing or resolved   | Call `mergePositions` on Polygon via Safe       |

## Migration Status

### Phase 1 (complete ✅)
- [x] Module scaffold, all packages defined
- [x] `internal/config` — reads `.env` (100% parity with Python config.py)
- [x] `internal/types` — all shared types
- [x] `internal/clob` — CLOB HTTP client + **EIP-712 signing** (manual, no SDK)
- [x] `internal/market` — MarketFinder (Gamma API)
- [x] `internal/pricer` — parallel REST pricer
- [x] `internal/ws/pricer` — WebSocket market feed
- [x] `internal/ws/user` — authenticated fill feed
- [x] `internal/fsm` — full FSM (ARB / MOMENTUM / MERGE logic)
- [x] `internal/inventory` — JSON-persisted token inventory + API reconcile
- [x] `internal/executor` — order placement + dry-run mode
- [x] `internal/merger` — on-chain MERGE via Gnosis Safe `execTransaction`
- [x] `cmd/bot/main.go` — full main loop

### Next Steps
- [ ] Integration test against testnet / mainnet with DRY_RUN=true
- [ ] Verify EIP-712 signatures match Python py_clob_client output
- [ ] Replace Python bot with Go binary
- [ ] Add Prometheus metrics endpoint

## Key Contracts (Polygon)

| Contract              | Address                                      |
|-----------------------|----------------------------------------------|
| ConditionalTokens     | `0x4D97DCd97eC945f40cF65F87097ACe5EA0476045` |
| USDC.e                | `0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174` |
| CTF Exchange          | `0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E` |
| Neg Risk CTF Exchange | `0xC5d563A36AE78145C45a50134d48A1215220f80a` |
| Neg Risk Adapter      | `0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296` |
