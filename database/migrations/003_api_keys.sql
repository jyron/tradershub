-- API keys are the billing and quota principal.
-- Existing bots are owner-created keys and should keep Pro access forever.
--
-- This migration rebuilds runs and every table that references runs so prod
-- data can be preserved while removing the old bots/accounts ownership model.

DROP TABLE IF EXISTS run_leaderboard_new;
DROP TABLE IF EXISTS run_results_new;
DROP TABLE IF EXISTS run_idempotency_new;
DROP TABLE IF EXISTS run_equity_new;
DROP TABLE IF EXISTS run_trades_new;
DROP TABLE IF EXISTS run_orders_new;
DROP TABLE IF EXISTS run_positions_new;
DROP TABLE IF EXISTS runs_new;

CREATE TABLE IF NOT EXISTS api_keys (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    api_key                TEXT UNIQUE NOT NULL,
    description            TEXT,
    creator_email          TEXT,
    created_at             TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active              INTEGER NOT NULL DEFAULT 1,
    disabled_reason        TEXT,
    plan                   TEXT NOT NULL DEFAULT 'free',
    stripe_customer_id     TEXT UNIQUE,
    stripe_subscription_id TEXT,
    subscription_status    TEXT,
    current_period_end     TEXT,
    billing_email          TEXT,
    handle                 TEXT
);

INSERT OR IGNORE INTO api_keys (
    id, name, api_key, description, creator_email, created_at,
    is_active, disabled_reason, plan
)
SELECT
    id, name, api_key, description, creator_email, created_at,
    is_active, disabled_reason, 'pro'
FROM bots;

CREATE INDEX IF NOT EXISTS idx_api_keys_api_key ON api_keys(api_key);
CREATE INDEX IF NOT EXISTS idx_api_keys_stripe_customer ON api_keys(stripe_customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_handle ON api_keys(handle) WHERE handle IS NOT NULL;

CREATE TABLE runs_new (
    id               TEXT PRIMARY KEY,
    api_key_id       TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    bot_name         TEXT,
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

INSERT INTO runs_new (
    id, api_key_id, bot_name, scenario_id, scenario_version, status,
    sim_time, cash, starting_cash, last_activity_at, created_at,
    completed_at, published
)
SELECT
    r.id, r.bot_id, b.name, r.scenario_id, r.scenario_version, r.status,
    r.sim_time, r.cash, r.starting_cash, r.last_activity_at, r.created_at,
    r.completed_at, r.published
FROM runs r
JOIN bots b ON b.id = r.bot_id;

CREATE TABLE run_positions_new (
    run_id     TEXT NOT NULL REFERENCES runs_new(id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    quantity   INTEGER NOT NULL,
    avg_cost   REAL NOT NULL,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, symbol)
);
INSERT INTO run_positions_new
SELECT * FROM run_positions;

CREATE TABLE run_orders_new (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES runs_new(id) ON DELETE CASCADE,
    symbol             TEXT NOT NULL,
    side               TEXT NOT NULL,
    quantity           INTEGER NOT NULL,
    reasoning          TEXT,
    queued_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    queued_at_sim_time TEXT NOT NULL
);
INSERT INTO run_orders_new
SELECT * FROM run_orders;

CREATE TABLE run_trades_new (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES runs_new(id) ON DELETE CASCADE,
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
INSERT INTO run_trades_new
SELECT * FROM run_trades;

CREATE TABLE run_equity_new (
    run_id          TEXT NOT NULL REFERENCES runs_new(id) ON DELETE CASCADE,
    sim_time        TEXT NOT NULL,
    cash            REAL NOT NULL,
    positions_value REAL NOT NULL,
    equity          REAL NOT NULL,
    PRIMARY KEY (run_id, sim_time)
);
INSERT INTO run_equity_new
SELECT * FROM run_equity;

CREATE TABLE run_idempotency_new (
    run_id        TEXT NOT NULL REFERENCES runs_new(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL,
    status_code   INTEGER NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, key)
);
INSERT INTO run_idempotency_new
SELECT * FROM run_idempotency;

CREATE TABLE run_results_new (
    run_id       TEXT PRIMARY KEY REFERENCES runs_new(id) ON DELETE CASCADE,
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
INSERT INTO run_results_new
SELECT * FROM run_results;

CREATE TABLE run_leaderboard_new (
    scenario_id  TEXT NOT NULL REFERENCES scenarios(id),
    run_id       TEXT NOT NULL REFERENCES runs_new(id),
    api_key_id   TEXT NOT NULL REFERENCES api_keys(id),
    bot_name     TEXT,
    return_pct   REAL NOT NULL,
    sharpe       REAL,
    published_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (scenario_id, run_id)
);

INSERT INTO run_leaderboard_new (
    scenario_id, run_id, api_key_id, bot_name, return_pct, sharpe, published_at
)
SELECT
    l.scenario_id, l.run_id, l.bot_id, b.name,
    l.return_pct, l.sharpe, l.published_at
FROM run_leaderboard l
JOIN bots b ON b.id = l.bot_id;

DROP TABLE run_leaderboard;
DROP TABLE run_results;
DROP TABLE run_idempotency;
DROP TABLE run_equity;
DROP TABLE run_trades;
DROP TABLE run_orders;
DROP TABLE run_positions;
DROP TABLE runs;

ALTER TABLE runs_new RENAME TO runs;
ALTER TABLE run_positions_new RENAME TO run_positions;
ALTER TABLE run_orders_new RENAME TO run_orders;
ALTER TABLE run_trades_new RENAME TO run_trades;
ALTER TABLE run_equity_new RENAME TO run_equity;
ALTER TABLE run_idempotency_new RENAME TO run_idempotency;
ALTER TABLE run_results_new RENAME TO run_results;
ALTER TABLE run_leaderboard_new RENAME TO run_leaderboard;

CREATE INDEX idx_runs_api_key ON runs(api_key_id);
CREATE INDEX idx_runs_status_activity ON runs(status, last_activity_at);
CREATE INDEX idx_run_orders_run ON run_orders(run_id);
CREATE INDEX idx_run_trades_run ON run_trades(run_id, sim_time_filled);
CREATE INDEX idx_idempotency_created ON run_idempotency(created_at);
CREATE INDEX idx_leaderboard_scenario
    ON run_leaderboard(scenario_id, return_pct DESC);

DROP TABLE bots;
DROP TABLE IF EXISTS accounts;
