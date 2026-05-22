# bottrade — Architecture & Repository Map

This file is the entry point for anyone (human or AI) opening this repo. It
covers what the project is, how the code is partitioned across two
deployable surfaces, the conventions that matter, and where to look for
things.

For **using** the public API as an external bot, read instead:
- `https://api.bot-trade.org/docs` — Swagger UI (auto-generated OpenAPI)
- `https://api.bot-trade.org/docs/agent.md` — integration guide written for agents
- `https://api.bot-trade.org/llms.txt` — discovery index for LLM clients

This file is about working *on* the code.

---

## What this project is

bottrade has two product surfaces sharing one Go monorepo and one Railway project:

1. **The website at https://bot-trade.org** — a public benchmark leaderboard
   where frontier LLM bots trade against live market data on a fixed
   system prompt. Visitors browse `/leaderboard`, `/models`, `/today`,
   `/methodology`. Hosted-bot submissions go through `/submit`.
2. **The Benchmark API at https://api.bot-trade.org** (`/v1/*`) — an HTTP
   service where any external agent (OpenClaw-style bots, custom
   LangGraph, ad-hoc scripts) brings its own setup and trades against a
   *frozen historical-bars scenario*. Deterministic step-based
   simulator. The agent runs anywhere; we only run the market.

Both surfaces are served by the **same Go binary**. Boot mode is selected
via the `SERVER_MODE` env var:

- `SERVER_MODE=site` — only the website (legacy `/api/*` routes on `$PORT`)
- `SERVER_MODE=api`  — only the benchmark API (`/v1/*` on `$PORT`)
- `SERVER_MODE=both` — both, site on `$PORT`, API on `$API_PORT` *(default for local dev)*

In production, two Railway services run this same image with different
`SERVER_MODE` values; that's the only difference.

---

## Code partition

The codebase splits into three buckets. Files are categorized by which
product surface depends on them. Most of the repo is site-only because
the site has been live longer; the API was added in 2026-05-22.

### Website-only (the `/api/*` legacy surface)

```
handlers/
  bots.go, trading.go, portfolio.go, leaderboard.go, seasons.go,
  assets.go, market.go, options.go, stats.go, vs.go, methodology.go,
  recap.go, rss.go, websocket.go, og.go, submit.go, backfill.go,
  page_meta.go, embed.go, cache.go

middleware/auth.go                          ← X-API-Key auth for /api/*

services/
  trading_engine.go, market_data.go, portfolio.go, seasons.go,
  keyvault.go, llm.go, og.go, options_service.go, assets_service.go,
  guardrails.go

models/
  bot.go, position.go, trade.go, asset.go, season.go, market.go, option.go

jobs/
  portfolio_snapshot.go, season_manager.go, daily_recap.go,
  asset_sync.go, backfill_runner.go, dynamic_bot_runner.go

database/migrations/001..014_*.sql

bots/                                       ← Python LLM client adapters
  claude_bot.py, gpt_bot.py, grok_bot.py, gemini_bot.py,
  openai_compat_bot.py, anthropic_compat_bot.py, google_compat_bot.py,
  common.py, baselines/, system_prompt.txt
```

### Benchmark API (the `/v1/*` surface — `api.bot-trade.org`)

```
handlers/apiv1/                             ← all /v1/* routes
  scenarios.go, runs.go, market.go, trades.go, step.go, results.go,
  publish.go, idempotency.go, errors.go, mount.go

middleware/auth_v1.go                       ← X-API-Key for /v1/* (no claim check, no daily cap)

services/
  scenario_engine.go                        ← simulator core (StartRun, QueueTrade,
                                              AdvanceStep, ComputeResults)
  scenario_bars.go                          ← in-process LRU cache of frozen bars
  scenario_provisioner.go                   ← FreezeScenario(id) — copies bars→scenario_bars
  scenario_universe.go                      ← the 50-symbol BenchmarkUniverse + slippage tiers

models/scenario.go, models/run.go

jobs/
  bar_ingest.go                             ← hourly Alpaca pull into market.bars
  idle_run_cleanup.go                       ← runs idle 5d → status='abandoned'
  run_results_compute.go                    ← compute metrics for finished runs
  idempotency_sweep.go                      ← prune run_idempotency older than 24h

database/migrations/015..018_*.sql          ← scenarios, runs, idempotency, results
database/migrations_market/                 ← separate DB for bars + scenario_bars

cmd/                                        ← one-shot CLIs for ops
  backfill_bars/                            ← Alpaca → market.bars (hourly OHLCV)
  provision_scenario/                       ← bars → scenario_bars + scenario row
  migrate/                                  ← apply migrations to any URL

scenarios/                                  ← committed scenario configs (JSON)
scripts/smoke_api.py                        ← Python end-to-end test client
services/scenario_engine_test.go            ← Go unit tests for the simulator
```

### Shared (both surfaces)

```
main.go                                     ← reads SERVER_MODE, mounts site and/or API
config/config.go                            ← env loader
database/db.go                              ← DB (app) and MarketDB (bars) pools
database/migrate.go                         ← RunMigrations + RunMigrationsOn
services/alpaca_client.go                   ← Alpaca SDK wrapper
services/market_history.go                  ← GetHistoricalCandles (Alpaca)
services/metrics.go                         ← Sharpe/Sortino/maxDD helpers
static/                                     ← the website's HTML/CSS/JS
```

The `bots` SQL table is also shared: every authenticated request (legacy
`/api/*` or new `/v1/*`) looks up an X-API-Key against this table. A
"bot" is the principal of authentication for both surfaces.

---

## Databases (two)

Both are Turso/libsql.

- **`tradershub`** (the app DB) — site state, bots, runs, scenarios catalog,
  run state, results, leaderboard. Set via `TURSO_DATABASE_URL` +
  `TURSO_AUTH_TOKEN`. Migrations in `database/migrations/`.
- **`tradershub-market`** (the market DB) — `bars` (raw cache) and
  `scenario_bars` (immutable per-scenario-version frozen bars). Only
  opened in api or both mode. Set via `TURSO_MARKET_DATABASE_URL` +
  `TURSO_MARKET_AUTH_TOKEN`. Migrations in
  `database/migrations_market/`. In local dev, defaults to
  `file:./bottrade-market.db`.

Both pools live on the `database` package: `database.DB` and
`database.MarketDB`. The benchmark API engine reads bars from MarketDB
and writes run state to DB.

---

## Conventions that matter

### Migrations are append-only and idempotent
- SQL files in `database/migrations*/` run in lexicographic order on boot.
- `database/migrate.go::execStatements` tolerates "duplicate column" errors
  so SQLite can simulate `ADD COLUMN IF NOT EXISTS`.
- Never edit a migration after it's been applied to prod. Always add a
  new numbered file.

### The legacy `/api/*` and the new `/v1/*` are isolated
- Never edit `handlers/trading.go`, `middleware/auth.go`, or migrations
  001..014 to "fix" something for the new API. Add new files in
  `handlers/apiv1/`, `middleware/auth_v1.go`, and migrations 015+.
- The simulator engine is in `services/scenario_engine.go` — completely
  separate from the live `services/trading_engine.go`. They don't share
  code.

### Auth pattern is shared but middlewares are separate
- Both surfaces validate `X-API-Key` against `bots.api_key` and stash
  the row at `c.Locals("bot", ...)`.
- The legacy middleware (`middleware/auth.go`) additionally requires
  `bot.Claimed = 1` and enforces a daily trade cap. The new one
  (`middleware/auth_v1.go`) does neither — benchmark API users are pure
  API consumers.

### The system prompt lives in one place
- `bots/system_prompt.txt` is the canonical text used by every site bot.
  Edits to this file change what every frontier-model bot sees. Don't
  copy-paste it into Go constants.

### Methodology page is served from the same file
- `/methodology` and `/api/methodology` both render
  `bots/system_prompt.txt`. Edits there are user-facing.

### Tests
- `go test ./...` runs everything. The simulator engine is the only
  unit-tested package right now (`services/scenario_engine_test.go`).
- The smoke test for the live API is `scripts/smoke_api.py`. It
  registers a bot, runs through a scenario end-to-end, and asserts
  idempotency. Used as a deploy-time sanity check.

### Local dev
- The full binary boots with no special env: it reads `.env.local` if
  present, falls back to local SQLite files (`bottrade.db`,
  `bottrade-market.db`) when Turso URLs aren't set.
- Hot iteration: `go run .` boots in mode=both by default.
- API-only local: `SERVER_MODE=api PORT=3099 go run .`.

---

## Operations

### Adding a new scenario to production

1. Drop a JSON file under `scenarios/<slug>.json`. Required keys: `slug`,
   `name`, `start_ts`, `end_ts`, `leverage_cap` ∈ {1,2,4,10}, `universe`.
   See `scenarios/tech-2024-q2.json` for a reference.
2. `export TURSO_MARKET_DATABASE_URL=…  TURSO_MARKET_AUTH_TOKEN=…`
3. `go run ./cmd/provision_scenario --config scenarios/<slug>.json`
4. The scenario is immediately visible at
   `https://api.bot-trade.org/v1/scenarios`.

You only re-run `cmd/backfill_bars` if you need a wider date range than
what's already in `market.bars` (currently 2024-01-02 → present) or you
added a new symbol to `services.BenchmarkUniverse`.

### Deploying

Both Railway services are GitHub-connected to `git@github.com:jyron/tradershub.git`.
Push to `main`, both deploy. Migrations apply idempotently on boot.

### Backups

The market DB (`tradershub-market`) is the only thing that costs real
time/money to recover — Alpaca data, ingested over months. Periodic dumps
of `scenario_bars` to object storage are worth setting up; not done yet.

---

## Where to look first

Use cases → starting files:

- "I'm changing the site leaderboard." → `handlers/leaderboard.go`, `static/leaderboard.html`
- "I'm modifying how a bot places a live trade." → `services/trading_engine.go`, `handlers/trading.go`
- "I'm changing how the benchmark simulator fills orders." → `services/scenario_engine.go::executeOrders`
- "I'm adding a new /v1 endpoint." → `handlers/apiv1/*.go` + `handlers/apiv1/mount.go`
- "I'm pulling more historical data from Alpaca." → `cmd/backfill_bars/main.go`, `services/market_history.go`
- "I'm provisioning a new scenario." → `scenarios/*.json`, `cmd/provision_scenario/main.go`
- "I'm onboarding a new frontier model bot." → `bots/common.py`, `bots/<model>_bot.py`, `handlers/submit.go`

Production endpoints + Turso DB names: see `memory/reference_prod_urls.md`
(Claude project memory).
