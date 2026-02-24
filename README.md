# polymarket-bot-go

Go port of the [polymarket-bot](https://github.com/gipsh/polymarket-bot) Python ARB/Momentum trading bot.

## Status: 🚧 In progress — skeleton only

The Python bot is currently running in production. This port will be developed in parallel and cut over once validated.

## Why Go

| | Python | Go |
|--|--|--|
| Concurrency | asyncio (complex) | goroutines (simple) |
| Deploy | Python + venv | single static binary |
| Memory | ~100MB | ~15MB |
| Latency | slower | ~10× faster |
| Type safety | runtime | compile-time |

## Architecture

```
cmd/bot/main.go          ← main loop, signal handling, market cycle
internal/
  config/                ← env vars → Config struct
  market/                ← Gamma API → []Market (active markets)
  pricer/                ← REST price fetcher (midpoints)
  clob/                  ← CLOB client (auth, orders, trades)
                           based on go-polymarket-tools/convergence-bot
  ws/
    pricer.go            ← WebSocket price feed (goroutine + channel)
    user.go              ← WebSocket authenticated fill feed
  fsm/                   ← Finite State Machine (ARB + Momentum logic)
  inventory/             ← JSON-persisted token balances + API reconcile
  executor/              ← Order placement + MERGE coordination
  onchain/               ← Gnosis Safe execTransaction → mergePositions
```

## Key Dependencies

```
github.com/polymarket/go-order-utils  ← EIP-712 order signing (official SDK)
github.com/ethereum/go-ethereum       ← on-chain operations (Gnosis Safe, CT)
github.com/gorilla/websocket          ← WebSocket feeds
github.com/joho/godotenv              ← .env loading
```

> **Note:** The CLOB client + executor WS are adapted from
> [gipsh/go-polymarket-tools](https://github.com/gipsh/go-polymarket-tools/tree/main/convergence-bot/polymarket)
> which already implements EIP-712 signing, HMAC headers, API key derivation,
> order posting and the authenticated fill WebSocket.

## Implementation Plan

### Phase 1 — Foundation (config, market, pricer)
- [ ] `config/config.go` — load `.env` into typed Config struct
- [ ] `market/finder.go` — call Gamma API, return `[]Market` closing within 4h
- [ ] `pricer/rest.go` — REST midpoint fetcher (fallback when WS is stale)

### Phase 2 — CLOB client (port from go-polymarket-tools)
- [ ] `clob/client.go` — copy + adapt from convergence-bot (already done)
- [ ] Add FOK market order support (convergence-bot only has GTC limit)
- [ ] Add `GetTrades()` endpoint (for inventory reconcile)
- [ ] Add `GetBalanceAllowance()` endpoint

### Phase 3 — Core logic
- [ ] `fsm/fsm.go` — ARB + Momentum FSM (1:1 port from Python)
- [ ] `inventory/inventory.go` — JSON persist + `ReconcileFromAPI()`
- [ ] `executor/executor.go` — `BuyMarket()`, `MergePairs()`, fill handling

### Phase 4 — WebSocket feeds
- [ ] `ws/pricer.go` — market price goroutine + in-memory cache
- [ ] `ws/user.go` — authenticated fill feed (port from convergence-bot executor)

### Phase 5 — On-chain / Merger
- [ ] `onchain/safe.go` — Gnosis Safe 1.3.0 `execTransaction` signing
- [ ] `onchain/merger.go` — `mergePositions` via Safe
- [ ] `onchain/setup.go` — `USDC.approve` × 3 contracts (setup tool)

### Phase 6 — Main loop
- [ ] `cmd/bot/main.go` — market cycle, goroutines, signal handling
- [ ] `Makefile` — build, run, test targets

## Configuration (`.env`)

Same as Python bot:

```env
PRIVATE_KEY=0x...           # MetaMask EOA private key
FUNDER_ADDRESS=0x...        # Gnosis Safe address (holds USDC)
SIGNATURE_TYPE=2            # 2=POLY_GNOSIS_SAFE
POLYGON_RPC=https://polygon-bor-rpc.publicnode.com
DRY_RUN=false
ARB_ORDER_USDC=5.0
MOMENTUM_MAIN_USDC=10.0
MOMENTUM_HEDGE_USDC=1.0
MOMENTUM_MAX_ENTRY=0.92
ARB_THRESHOLD=0.97
MOMENTUM_TRIGGER=0.85
```

## Validation Strategy

1. Run Go bot with `DRY_RUN=true` alongside the Python bot
2. Compare FSM decisions for the same market data
3. Cut over when behavior matches for 48h
