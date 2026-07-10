-- Initial schema for the Benchmark API.
-- All previous migrations (001..018) collapsed into one. The legacy
-- /api/* tables (positions, trades, portfolio_snapshots, ranking_history,
-- seasons, account_isolations, daily_recaps, bot_credentials,
-- backfill_jobs, bot_usage_daily) are intentionally not recreated.

-- Auth principal. Self-issued via POST /v1/keys (no other onboarding).
CREATE TABLE IF NOT EXISTS bots (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    api_key         TEXT UNIQUE NOT NULL,
    description     TEXT,
    creator_email   TEXT,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active       INTEGER NOT NULL DEFAULT 1,
    tier            TEXT NOT NULL DEFAULT 'challenger',
    disabled_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_bots_api_key ON bots(api_key);

-- Scenario catalog. A scenario is a fixed historical market window with a
-- fixed universe, leverage cap, and per-symbol slippage tier.
-- scenario_versions records every re-freeze so in-flight runs stay pinned.
CREATE TABLE IF NOT EXISTS scenarios (
    id               TEXT PRIMARY KEY,
    slug             TEXT UNIQUE NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    bar_resolution   TEXT NOT NULL DEFAULT '1Hour',
    start_ts         TEXT NOT NULL,
    end_ts           TEXT NOT NULL,
    starting_cash    REAL NOT NULL DEFAULT 100000,
    leverage_cap     REAL NOT NULL DEFAULT 1.0,
    short_enabled    INTEGER NOT NULL DEFAULT 1,
    universe_json    TEXT NOT NULL,
    slippage_json    TEXT NOT NULL,
    benchmark_symbol TEXT NOT NULL DEFAULT 'SPY',
    status           TEXT NOT NULL DEFAULT 'draft',
    current_version  INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_scenarios_status ON scenarios(status);

CREATE TABLE IF NOT EXISTS scenario_versions (
    scenario_id    TEXT NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    version        INTEGER NOT NULL,
    bars_frozen_at TEXT NOT NULL,
    bar_count      INTEGER NOT NULL,
    PRIMARY KEY (scenario_id, version)
);

-- Per-run mutable state. quantity is SIGNED: negative = short.
CREATE TABLE IF NOT EXISTS runs (
    id               TEXT PRIMARY KEY,
    bot_id           TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    scenario_id      TEXT NOT NULL REFERENCES scenarios(id),
    scenario_version INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    sim_time         TEXT NOT NULL,
    cash             REAL NOT NULL,
    starting_cash    REAL NOT NULL,
    last_activity_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at     TEXT,
    published        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_runs_bot ON runs(bot_id);
CREATE INDEX IF NOT EXISTS idx_runs_status_activity ON runs(status, last_activity_at);

CREATE TABLE IF NOT EXISTS run_positions (
    run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    quantity   INTEGER NOT NULL,
    avg_cost   REAL NOT NULL,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, symbol)
);

CREATE TABLE IF NOT EXISTS run_orders (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    symbol             TEXT NOT NULL,
    side               TEXT NOT NULL,
    quantity           INTEGER NOT NULL,
    reasoning          TEXT,
    queued_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    queued_at_sim_time TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_orders_run ON run_orders(run_id);

CREATE TABLE IF NOT EXISTS run_trades (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    symbol          TEXT NOT NULL,
    side            TEXT NOT NULL,
    quantity        INTEGER NOT NULL,
    fill_price      REAL NOT NULL,
    slippage_bps    INTEGER NOT NULL,
    sim_time_filled TEXT NOT NULL,
    total_value     REAL NOT NULL,
    realized_pnl    REAL NOT NULL DEFAULT 0,
    reasoning       TEXT
);
CREATE INDEX IF NOT EXISTS idx_run_trades_run ON run_trades(run_id, sim_time_filled);

CREATE TABLE IF NOT EXISTS run_equity (
    run_id          TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sim_time        TEXT NOT NULL,
    cash            REAL NOT NULL,
    positions_value REAL NOT NULL,
    equity          REAL NOT NULL,
    PRIMARY KEY (run_id, sim_time)
);

-- Idempotency. Rows are swept after 24h by the idempotency_sweep job.
CREATE TABLE IF NOT EXISTS run_idempotency (
    run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL,
    status_code   INTEGER NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, key)
);
CREATE INDEX IF NOT EXISTS idx_idempotency_created ON run_idempotency(created_at);

-- Post-run metrics + public leaderboard.
CREATE TABLE IF NOT EXISTS run_results (
    run_id       TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    final_equity REAL NOT NULL,
    return_pct   REAL NOT NULL,
    sharpe       REAL,
    sortino      REAL,
    max_drawdown REAL,
    volatility   REAL,
    trade_count  INTEGER NOT NULL,
    liquidated   INTEGER NOT NULL DEFAULT 0,
    computed_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS run_leaderboard (
    scenario_id  TEXT NOT NULL REFERENCES scenarios(id),
    run_id       TEXT NOT NULL REFERENCES runs(id),
    bot_id       TEXT NOT NULL REFERENCES bots(id),
    return_pct   REAL NOT NULL,
    sharpe       REAL,
    published_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (scenario_id, run_id)
);
CREATE INDEX IF NOT EXISTS idx_leaderboard_scenario
    ON run_leaderboard(scenario_id, return_pct DESC);
