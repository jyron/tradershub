-- Benchmark API: scenario catalog.
-- A scenario is a frozen historical market window with a fixed universe,
-- leverage cap, and slippage tier per symbol. External agents start runs
-- against a scenario and trade against its frozen bars.
--
-- scenario_versions records every time a scenario's bars are re-frozen,
-- so any in-flight run can stay pinned to the version it started under.
CREATE TABLE IF NOT EXISTS scenarios (
    id               TEXT PRIMARY KEY,
    slug             TEXT UNIQUE NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    bar_resolution   TEXT NOT NULL DEFAULT '1Hour',
    start_ts         TEXT NOT NULL,
    end_ts           TEXT NOT NULL,
    starting_cash    REAL NOT NULL DEFAULT 100000,
    leverage_cap     REAL NOT NULL DEFAULT 1.0,    -- 1 | 2 | 4 | 10
    short_enabled    INTEGER NOT NULL DEFAULT 1,
    universe_json    TEXT NOT NULL,                -- JSON array of symbols
    slippage_json    TEXT NOT NULL,                -- {"AAPL":5,"PLTR":15,...} bps overrides
    benchmark_symbol TEXT NOT NULL DEFAULT 'SPY',
    status           TEXT NOT NULL DEFAULT 'draft',-- draft | provisioning | ready | archived
    current_version  INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scenario_versions (
    scenario_id    TEXT NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    version        INTEGER NOT NULL,
    bars_frozen_at TEXT NOT NULL,
    bar_count      INTEGER NOT NULL,
    PRIMARY KEY (scenario_id, version)
);

CREATE INDEX IF NOT EXISTS idx_scenarios_status ON scenarios(status);
