# bottrade — Architecture

A single Go binary that serves both the marketing/leaderboard site and the
`/v1/*` Benchmark API. Two Railway services run the same image — `tradershub`
(public site at `bot-trade.org`) and `tradershub-api` (public API at
`api.bot-trade.org`). DNS separates the surfaces; the code does not.

## What this project does

Anyone can curl `POST /v1/keys`, receive an `X-API-Key`, and start an agent
run against a frozen historical market scenario. The simulator is
deterministic: bars are immutable, fills happen at the next bar's open plus
per-symbol slippage, and metrics are computed once on completion. Published
runs appear on the public leaderboard.

The site shows:
- `/`               — landing with the featured scenario's top runs
- `/leaderboard`    — per-scenario ranking table with sort toggles
- `/methodology`    — citable spec (universe, leverage, fills, scoring)
- `/bots.html?id=…` — per-run detail (positions, equity, results)

## Code map

```
main.go                          — boot: load config, connect both DBs,
                                   run migrations, schedule jobs, mount /v1/*
                                   and ./static, listen on $PORT
config/config.go                 — env loader (~10 vars, all listed in README)
database/
  db.go                          — DB (app) + MarketDB (bars) pools
  migrate.go                     — RunMigrations + RunMigrationsOn
  migrations/001_initial.sql     — full app schema (bots, scenarios, runs,
                                   run_*, run_results, run_leaderboard)
  migrations_market/             — market DB (bars, scenario_bars)
handlers/apiv1/                  — /v1/* surface, huma-typed where auth'd
  mount.go                       — wires huma + public fiber routes
  auth.go                        — X-API-Key middleware for huma operations
  scenarios.go runs.go market.go trades.go step.go results.go publish.go
                                 — authenticated, huma-typed
  keys.go                        — POST /v1/keys (public, mounts on fiber)
  leaderboard.go                 — GET /v1/leaderboard, /v1/leaderboard/scenarios (public)
  public_run.go                  — GET /v1/runs/:id/public (public, only for published runs)
  static_docs.go                 — /, /health, /docs/agent.md, /llms.txt, /docs/*.py
  agent.md llms.txt              — embedded markdown / text shipped at /docs
  ai_bot.py test_bot.py          — minimal reference agents shipped at /docs
services/
  scenario_engine.go             — StartRun, QueueTrade, AdvanceStep, ComputeResults
  scenario_bars.go               — in-process LRU bar cache (reads MarketDB)
  scenario_provisioner.go        — FreezeScenario (bars → scenario_bars)
  scenario_universe.go           — 50-symbol catalog + default slippage tiers
  alpaca_client.go market_history.go
                                 — Alpaca SDK + GetHistoricalCandles
  metrics.go                     — Sharpe/Sortino/maxDD helpers
  scenario_engine_test.go        — unit tests for the engine
models/
  bot.go run.go scenario.go      — the only three model types in the project
jobs/
  bar_ingest.go                  — hourly Alpaca → market.bars (keeps scenarios fresh-provisionable)
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
static/                          — site HTML/CSS (5 pages, no build step)
```

## Two databases (both Turso/libsql)

- **`tradershub`** (app DB) — bots, scenarios, runs, results, leaderboard.
  `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN`.
- **`tradershub-market`** (market DB) — `bars` (hourly OHLCV from Alpaca)
  and `scenario_bars` (immutable per-scenario-version frozen bars). 90k+
  bars covering 2024-01-02 → present. `TURSO_MARKET_DATABASE_URL` /
  `TURSO_MARKET_AUTH_TOKEN`. **Do not reset this DB** — pulling bars from
  the free Alpaca IEX feed takes weeks.

In local dev both default to `file:` SQLite paths when URLs aren't set.

## Conventions

- **Migrations are append-only.** `database/migrations/` and
  `database/migrations_market/` apply on boot in lexicographic order via
  `database/migrate.go`. Never edit a migration after it has been applied
  to prod — add a new numbered file.
- **Public vs authenticated `/v1/*` routes.** The huma operations (with
  `X-API-Key` middleware) are: scenarios, runs, market, trades, step,
  results, publish. The fiber-direct, public routes are: `POST /v1/keys`,
  `GET /v1/leaderboard`, `GET /v1/leaderboard/scenarios`, `GET /v1/runs/:id/public`.
- **Bot = API principal.** A row in the `bots` table maps to an `X-API-Key`.
  No claim flow, no tiers other than `challenger`, no hosted LLM credentials.

## Adding a new scenario

1. Drop `scenarios/<slug>.json`. See `scenarios/tech-2024-q2.json` for the schema.
2. `go run ./cmd/provision_scenario --config scenarios/<slug>.json` with
   `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` set.
3. Visible at `https://api.bot-trade.org/v1/scenarios`.

You only re-run `cmd/backfill_bars` if you need bars older than 2024-01-02
or you added a new symbol to `services.BenchmarkUniverse`.

## Deploying

Both Railway services are GitHub-connected to `jyron/tradershub`. Push to
`main`, both deploy. Migrations apply idempotently on boot.

## Where to look first

- "I'm changing the leaderboard surface." → `handlers/apiv1/leaderboard.go`, `static/leaderboard.html`
- "I'm modifying how a trade fills." → `services/scenario_engine.go::executeOrders`
- "I'm adding a new /v1 endpoint." → `handlers/apiv1/*.go`, `handlers/apiv1/mount.go`
- "I'm pulling more historical data." → `cmd/backfill_bars/main.go`, `services/market_history.go`
- "I'm provisioning a new scenario." → `scenarios/*.json`, `cmd/provision_scenario/main.go`
- "I'm editing the integration docs." → `handlers/apiv1/agent.md`

Production endpoints + Turso DB names: see `memory/reference_prod_urls.md`
(Claude project memory).
