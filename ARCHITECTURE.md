# bottrade — Architecture

A single Go binary on a single Railway service (`tradershub`) that serves
both the marketing site (`/`, `/leaderboard`, `/methodology`, `/run/:id`)
and the Benchmark API (`/api/*`). One domain, one binary, one process.

## What this project does

Anyone can curl `POST /api/v1/keys`, receive an `X-API-Key`, and start an
agent run against a frozen historical market scenario. The simulator is
deterministic: bars are immutable, fills happen at the next bar's open
plus per-symbol slippage, and metrics are computed once on completion.
Published runs appear on the public leaderboard.

The site shows:
- `/`               — landing with the featured scenario's top runs
- `/leaderboard`    — per-scenario ranking table with sort toggles
- `/methodology`    — citable spec (universe, leverage, fills, scoring)
- `/scenarios`      — scenario catalog


## Routing summary

| Path                                       | Auth     | Notes                                      |
|--------------------------------------------|----------|--------------------------------------------|
| `/`, `/leaderboard`, `/methodology`        | none     | Marketing pages (served from `static/`)    |
| `/api/docs`                                | none     | Swagger UI (huma-generated)                |
| `/api/openapi.json`                        | none     | OpenAPI spec                                |
| `/api/agent-skills.md`                     | none     | Agent integration guide                     |
| `/api/llms.txt`                            | none     | LLM discovery file                          |
| `/api/test_bot.py` / `/api/ai_bot.py`      | none     | Reference agents                            |
| `/api/health`                              | none     | Health check                                |
| `POST /api/v1/keys`                        | none     | Self-serve key issuer                       |
| `GET /api/v1/scenarios` (+ `/:id`)         | none     | Scenario catalog                            |
| `GET /api/v1/leaderboard` (+ `/scenarios`) | none     | Public per-scenario ranking                 |
| `GET /api/v1/runs/:id/public`              | none     | Read-only view of a published run           |
| `POST /api/v1/runs`                        | X-API-Key | Start a run                                |
| `GET /api/v1/runs/:id`                     | X-API-Key | Run snapshot (owner only)                  |
| `GET /api/v1/runs/:id/market`              | X-API-Key | Read bars at current sim_time              |
| `POST /api/v1/runs/:id/trades`             | X-API-Key | Queue orders                                |
| `POST /api/v1/runs/:id/step`               | X-API-Key | Advance sim_time                            |
| `GET /api/v1/runs/:id/results`             | X-API-Key | Computed metrics                            |
| `POST /api/v1/runs/:id/publish`            | X-API-Key | Opt into public leaderboard                 |
| `POST /api/v1/billing/checkout`            | optional X-API-Key | Returns Stripe Checkout URL        |
| `POST /api/v1/billing/portal`              | X-API-Key | Returns Stripe Customer Portal URL          |
| `GET /api/v1/billing/account`              | X-API-Key | API key billing info                        |
| `PATCH /api/v1/billing/account`            | X-API-Key | Set leaderboard handle (Pro only)           |

## Code map

```
main.go                          — boot: load config, connect both DBs,
                                   run migrations, schedule jobs, mount
                                   /api/* and ./static, listen on $PORT
config/config.go                 — env loader
database/
  db.go                          — DB (app) + MarketDB (bars) pools
  migrate.go                     — RunMigrations + RunMigrationsOn
  migrations/001_initial.sql     — legacy bootstrap schema
  migrations/003_api_keys.sql    — current app schema shape (api_keys,
                                   runs, run_*, run_results, leaderboard)
  migrations/004_drop_idempotency_status_code.sql
                                 — removes unused idempotency status bookkeeping
  migrations_market/             — market DB (bars, scenario_bars)
handlers/apiv1/                  — /api/* surface, huma-typed where auth'd
  mount.go                       — wires huma + public fiber routes
  auth.go                        — X-API-Key middleware (bypasses GET /api/v1/scenarios)
  scenarios.go runs.go market.go trades.go step.go results.go publish.go
                                 — huma operations
  keys.go                        — POST /api/v1/keys (public, fiber-direct)
  leaderboard.go                 — GET /api/v1/leaderboard{,/scenarios} (public, fiber-direct)
  public_run.go                  — GET /api/v1/runs/:id/public (public, fiber-direct)
  static_docs.go                 — /api/health, /api/agent-skills.md, /api/llms.txt,
                                   /api/test_bot.py, /api/ai_bot.py
  agent-skills.md llms.txt       — embedded markdown / text shipped at /api
  ai_bot.py test_bot.py          — reference agents shipped at /api
services/
  scenario_engine.go             — StartRun, QueueTrade, AdvanceStep, ComputeResults
  scenario_bars.go               — in-process bar cache (reads MarketDB)
  scenario_provisioner.go        — FreezeScenario (bars → scenario_bars)
  scenario_universe.go           — equity catalog (BenchmarkUniverse) + crypto
                                   pairs (CryptoUniverse) + IsCryptoSymbol +
                                   default slippage tiers
  alpaca_client.go market_history.go
                                 — Alpaca SDK + GetHistoricalCandles (equities)
                                   and GetHistoricalCryptoCandles (24/7 crypto)
  scenario_engine_test.go        — unit tests for the engine
models/api_key.go run.go scenario.go — the three model types
jobs/
  bar_ingest.go                  — hourly Alpaca → market.bars (equities + crypto)
  idempotency_sweep.go           — prune run_idempotency rows older than 24h
  idle_run_cleanup.go            — runs idle 5d → status='abandoned'
  run_results_compute.go         — compute Sharpe/Sortino/maxDD for finished runs
  scheduler.go                   — minimal job loop
cmd/
  backfill_bars/                 — one-shot: Alpaca → market.bars (historical)
  provision_scenario/            — one-shot: bars → scenario_bars + scenario row
  migrate/                       — one-shot: apply migrations to any DB URL
scenarios/                       — committed scenario configs (JSON)
scripts/smoke_api.py             — end-to-end Python client used as a deploy smoke
static/                          — site HTML/CSS (4 pages, no build step)
```

## Two databases (both Turso/libsql)

- **`tradershub-v2`** (app DB) — API keys, scenarios, runs, results, leaderboard.
  `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN`. App migrations end at
  `database/migrations/004_drop_idempotency_status_code.sql`.
- **`tradershub-market`** (market DB) — `bars` (hourly OHLCV from Alpaca, both
  equities and crypto pairs like `BTC/USD`) and `scenario_bars` (immutable
  per-scenario-version frozen bars). 90k+ bars covering 2024-01-02 → present
  for equities; crypto history reaches back to ~2021. `TURSO_MARKET_DATABASE_URL` /
  `TURSO_MARKET_AUTH_TOKEN`. **Do not reset this DB** — pulling bars from
  the free Alpaca feed takes weeks. `volume` is stored as a REAL (crypto coin
  volume is fractional); equity/crypto bars share the table, keyed by symbol.

In local dev both default to `file:` SQLite paths when URLs aren't set.

## Conventions

- **Migrations are append-only.** `database/migrations/` and
  `database/migrations_market/` apply on boot in lexicographic order via
  `database/migrate.go`. Never edit a migration after it has been applied
  to prod — add a new numbered file.
- **Public vs authenticated routes.** Public, no-auth: every `/api/...`
  doc route, `POST /api/v1/keys`, `POST /api/v1/billing/checkout`, all `GET /api/v1/scenarios*` and
  `/api/v1/leaderboard*`, and `GET /api/v1/runs/:id/public`. Everything
  else under `/api/v1/runs/*` requires `X-API-Key`.
- **API key = API principal.** A row in `api_keys` maps to an `X-API-Key`,
  subscription state, and one monthly usage bucket. Any number of strategies
  or scripts can use the same key. No hosted LLM credentials.

## Adding a new scenario

1. Drop `scenarios/<slug>.json`. See `scenarios/tech-2024-q2.json` (equities)
   or `scenarios/ftx-collapse-2022.json` (crypto). A crypto scenario lists
   `BTC/USD`-style pairs in its `universe` and a crypto `benchmark_symbol`;
   keep crypto and equity symbols in separate scenarios so the timeline (the
   union of all symbols' bars) stays on one 24/7 or one RTH grid.
2. Ensure the window's bars are backfilled (see below), then
   `go run ./cmd/provision_scenario --config scenarios/<slug>.json`
   with `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` set.
3. Visible at `https://bot-trade.org/api/v1/scenarios`.

Run `cmd/backfill_bars` if you need bars the rolling ingest hasn't pulled —
equity history older than 2024-01-02, a new `services.BenchmarkUniverse`
symbol, or a crypto window (pass the pairs explicitly, e.g.
`--symbols BTC/USD,ETH/USD`). Quantities are fractional end-to-end, so crypto
agents can hold e.g. 0.25 BTC.

## Deploying

The Railway service `tradershub` is GitHub-connected to
`jyron/tradershub`. Push to `main`, it deploys. Migrations apply
idempotently on boot.

## Where to look first

- "Leaderboard surface." → `handlers/apiv1/leaderboard.go`, `static/leaderboard.html`
- "How a trade fills." → `services/scenario_engine.go::executeOrdersTx`
- "Add a new `/api/v1` endpoint." → `handlers/apiv1/*.go`, `handlers/apiv1/mount.go`
- "Pull more historical data." → `cmd/backfill_bars/main.go`, `services/market_history.go`
- "Provision a new scenario." → `scenarios/*.json`, `cmd/provision_scenario/main.go`
- "Edit the integration docs." → `handlers/apiv1/agent-skills.md`
