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

## Code map

```
main.go                          — boot: load config, connect both DBs,
                                   run migrations, schedule jobs, mount
                                   /api/* and ./static, listen on $PORT
config/config.go                 — env loader
database/
  db.go                          — DB (app) + MarketDB (bars) pools
  migrate.go                     — RunMigrations + RunMigrationsOn
  migrations/001_initial.sql     — full app schema (bots, scenarios, runs,
                                   run_*, run_results, run_leaderboard)
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
  scenario_bars.go               — in-process LRU bar cache (reads MarketDB)
  scenario_provisioner.go        — FreezeScenario (bars → scenario_bars)
  scenario_universe.go           — 50-symbol catalog + default slippage tiers
  alpaca_client.go market_history.go
                                 — Alpaca SDK + GetHistoricalCandles
  metrics.go                     — Sharpe/Sortino/maxDD helpers
  scenario_engine_test.go        — unit tests for the engine
models/bot.go run.go scenario.go — the three model types
jobs/
  bar_ingest.go                  — hourly Alpaca → market.bars
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

- **`tradershub-v2`** (app DB) — bots, scenarios, runs, results, leaderboard.
  `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN`. Schema: a single
  `database/migrations/001_initial.sql`.
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
- **Public vs authenticated routes.** Public, no-auth: every `/api/...`
  doc route, `POST /api/v1/keys`, all `GET /api/v1/scenarios*` and
  `/api/v1/leaderboard*`, and `GET /api/v1/runs/:id/public`. Everything
  else under `/api/v1/runs/*` requires `X-API-Key`.
- **Bot = API principal.** A row in the `bots` table maps to an
  `X-API-Key`. No claim flow, no tiers other than `challenger`, no
  hosted LLM credentials.

## Adding a new scenario

1. Drop `scenarios/<slug>.json`. See `scenarios/tech-2024-q2.json`.
2. `go run ./cmd/provision_scenario --config scenarios/<slug>.json`
   with `TURSO_MARKET_DATABASE_URL` / `TURSO_MARKET_AUTH_TOKEN` set.
3. Visible at `https://bot-trade.org/api/v1/scenarios`.

You only re-run `cmd/backfill_bars` if you need bars older than
2024-01-02 or you added a new symbol to `services.BenchmarkUniverse`.

## Deploying

The Railway service `tradershub` is GitHub-connected to
`jyron/tradershub`. Push to `main`, it deploys. Migrations apply
idempotently on boot.

## Where to look first

- "Leaderboard surface." → `handlers/apiv1/leaderboard.go`, `static/leaderboard.html`
- "How a trade fills." → `services/scenario_engine.go::executeOrders`
- "Add a new `/api/v1` endpoint." → `handlers/apiv1/*.go`, `handlers/apiv1/mount.go`
- "Pull more historical data." → `cmd/backfill_bars/main.go`, `services/market_history.go`
- "Provision a new scenario." → `scenarios/*.json`, `cmd/provision_scenario/main.go`
- "Edit the integration docs." → `handlers/apiv1/agent-skills.md`

Production endpoints + Turso DB names: see `memory/reference_prod_urls.md`
(Claude project memory).
