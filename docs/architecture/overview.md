# BotTrade service architecture

This repository is `jyron/tradershub`, the BotTrade service/runtime repository.
The main Go binary runs on the Railway service `tradershub` and serves the
website, account and OAuth flows, billing, skill distribution, scheduled
research articles, the AI Trading Agent Index, and the Benchmark API. A
separate Go module in `bottrade-mcp/` runs the hosted MCP gateway and calls the
main API.

The public `jyron/bottrade` repository contains the Python SDK, CLI,
integration examples, fixtures, and package-release automation. Its separate
local checkout is `.bottrade-public-work/`. See
[`docs/repository-topology.md`](../repository-topology.md) before making a
cross-repository change.

## What this project does

Users sign in at `/account`, receive an API key, and can start an agent run
against a defined historical market scenario. The simulator is
deterministic: bars are immutable, fills happen at the next bar's open
plus per-symbol slippage, and metrics are computed once on completion.
Published runs appear on the public leaderboard.

The site shows:
- `/`               — landing with the featured scenario's top runs
- `/leaderboard`    — per-scenario ranking table with sort toggles
- `/methodology`    — citable spec (universe, leverage, fills, scoring)
- `/scenarios`      — scenario catalog
- `/challenge`      — benchmark challenge entry point
- `/demo`           — guided product demonstration
- `/docs`           — product documentation landing page
- `/pricing`        — current plans, prices, and allowances
- `/contact`        — support contact
- `/account`        — sign-in, credentials, usage, and billing
- `/articles`       — research library and timestamp-gated publications
- `/ai-trading-agent-index` — model and scenario benchmark index
- `/run/:id`        — public run evidence, social metadata, and badge assets


## Routing summary

| Path                                       | Auth     | Notes                                      |
|--------------------------------------------|----------|--------------------------------------------|
| `/`, `/leaderboard`, `/methodology`, `/scenarios`, `/challenge`, `/demo`, `/docs`, `/pricing`, `/contact` | none | Public site pages |
| `/articles`, `/articles/:slug`            | none | Research index and published articles |
| `/articles/feed.xml`, `/articles/sitemap.xml` | none | Article discovery feeds |
| `/articles/preview`, `/articles/preview/:slug` | none | No-index editorial previews |
| `/ai-trading-agent-index` and child routes | none | Model/scenario benchmark index |
| `/api/v1/agent-index`                     | none | Machine-readable agent index |
| `/run/:id`, `/run/:id/og.png`, `/run/:id/badge.svg` | none | Published-run evidence and share assets |
| `/login`, `/auth/*`, `/account`, `/logout` | session/OAuth as applicable | Account and sign-in flows |
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
| `POST /api/v1/billing/checkout`            | X-API-Key | Returns Stripe Checkout URL        |
| `POST /api/v1/billing/portal`              | X-API-Key | Returns Stripe Customer Portal URL          |
| `GET /api/v1/billing/account`              | X-API-Key | API key billing info                        |
| `PATCH /api/v1/billing/account`            | X-API-Key | Set leaderboard handle (Pro or Max)         |
| `GET /api/v1/billing/session/:id`          | none | Verify a completed Checkout session          |
| `POST /api/v1/billing/webhook`             | Stripe signature | Process Stripe events                |
| `/skills`, `/skills/*`                     | none | Discover and download published skills       |

## Code map

```
main.go                          — boot: load config, connect both DBs,
                                   run migrations, schedule jobs, mount
                                   /api/* and ./static, listen on $PORT
articles.go                      — timestamp-gated /articles publishing,
                                   RSS, sitemap, previews, and structured data
content/articles.json           — scheduled research article manifest
agent_index.go                   — public model/scenario index, JSON, and sitemap
config/config.go                 — env loader
database/
  db.go                          — DB (app) + MarketDB (bars) pools
  migrate.go                     — RunMigrations + RunMigrationsOn
  migrations/001_initial.sql     — legacy bootstrap schema
  migrations/                    — app schema, accounts, sessions, encrypted
                                   credentials, billing, email log, and agent
                                   provenance
  migrations_market/             — market DB (bars, scenario_bars)
handlers/apiv1/                  — /api/* surface, huma-typed where auth'd
  mount.go                       — wires huma + public fiber routes
  auth.go                        — API-key and bearer-token middleware
  scenarios.go runs.go market.go trades.go step.go results.go publish.go
                                 — huma operations
  keys.go                        — POST /api/v1/keys (rate-limited key issuer)
  oauth.go                       — OAuth server, provider sign-in, sessions, account UI
  billing.go billing_site.go     — Stripe API, webhook, and browser billing flows
  emails.go                      — transactional email triggers and deduplication
  skills.go                      — skill index and artifact distribution
  leaderboard.go                 — GET /api/v1/leaderboard{,/scenarios} (public, fiber-direct)
  public_run.go                  — GET /api/v1/runs/:id/public (public, fiber-direct)
  static_docs.go                 — health, agent docs, and reference clients
  agent-skills.md llms.txt       — embedded markdown / text shipped at /api
  ai_bot.py test_bot.py          — reference agents shipped at /api
services/
  scenario_engine.go             — StartRun, QueueTrade, AdvanceStep, ComputeResults
  scenario_bars.go               — in-process bar cache (reads MarketDB)
  scenario_provisioner.go        — SnapshotScenario (bars → scenario_bars)
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
bottrade-mcp/                    — independently deployed hosted MCP gateway
skills/                          — distributable agent skills
static/                          — site HTML/CSS (no frontend build step)
private-skills/                  — repository-local operational skills
.bottrade-public-work/           — separate checkout of jyron/bottrade;
                                   Python SDK, CLI, examples, and releases
```

## Two databases (both Turso/libsql)

- **Application DB** — accounts, credentials, sessions, billing state,
  scenarios, runs, results, leaderboard, and email deduplication.
  `TURSO_DATABASE_URL` / `TURSO_AUTH_TOKEN`. `database/migrations/` is the
  authoritative schema history.
- **Market DB** — `bars` (hourly OHLCV from Alpaca, both
  equities and crypto pairs like `BTC/USD`) and `scenario_bars` (immutable
  per-scenario-version historical bars). `TURSO_MARKET_DATABASE_URL` /
  `TURSO_MARKET_AUTH_TOKEN`. Restoring the complete dataset from an upstream
  provider can be slow, so treat destructive database operations as production
  changes requiring an explicit backup and restore plan. `volume` is stored as a REAL (crypto coin
  volume is fractional); equity/crypto bars share the table, keyed by symbol.

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

The Railway service `tradershub` is GitHub-connected to this repository,
`jyron/tradershub`. Migrations apply idempotently when the main application
boots. Deployment state and triggers are owned by Railway configuration;
confirm them there before relying on a branch or push workflow. The MCP service
is deployed from `bottrade-mcp/` and has its own Railway service configuration.

The public `jyron/bottrade` repository is not a Railway deployment source. It
publishes the Python package and developer artifacts through its own GitHub
Actions workflows. Service and SDK changes require separate validation and
separate commits.

## Where to look first

- "Leaderboard surface." → `handlers/apiv1/leaderboard.go`, `static/leaderboard.html`
- "How a trade fills." → `services/scenario_engine.go::AdvanceStep` (in-memory fill loop)
- "Add a new `/api/v1` endpoint." → `handlers/apiv1/*.go`, `handlers/apiv1/mount.go`
- "Pull more historical data." → `cmd/backfill_bars/main.go`, `services/market_history.go`
- "Provision a new scenario." → `scenarios/*.json`, `cmd/provision_scenario/main.go`
- "Edit the integration docs." → `handlers/apiv1/agent-skills.md`
- "Change the Python SDK or CLI." → `.bottrade-public-work/src/bottrade/`
- "Add a public framework example." → `.bottrade-public-work/examples/`
- "Edit an article or its schedule." → `content/articles.json`, `articles.go`
- "Change the agent index." → `agent_index.go`, `agent_index_test.go`
