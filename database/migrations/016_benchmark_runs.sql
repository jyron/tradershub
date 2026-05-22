-- Benchmark API: per-run mutable state.
-- A run is one (bot, scenario, scenario_version) tuple traversing the
-- scenario's frozen bars. All run state is keyed by run_id; the engine
-- never reads or writes the legacy positions/trades tables.
--
-- run_positions.quantity is SIGNED — negative means short.
-- run_orders is the queue between POST /trades and the next /step;
-- run_trades is the immutable fill record.

CREATE TABLE IF NOT EXISTS runs (
    id               TEXT PRIMARY KEY,
    bot_id           TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    scenario_id      TEXT NOT NULL REFERENCES scenarios(id),
    scenario_version INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',  -- active|completed|liquidated|abandoned
    sim_time         TEXT NOT NULL,                    -- current bar timestamp; next /step advances past this
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
    quantity   INTEGER NOT NULL,           -- SIGNED: negative = short
    avg_cost   REAL NOT NULL,              -- entry basis; recomputed on adds
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, symbol)
);

CREATE TABLE IF NOT EXISTS run_orders (   -- queued, awaiting next /step
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    symbol             TEXT NOT NULL,
    side               TEXT NOT NULL,      -- buy | sell | short | cover
    quantity           INTEGER NOT NULL,
    reasoning          TEXT,
    queued_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    queued_at_sim_time TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_run_orders_run ON run_orders(run_id);

CREATE TABLE IF NOT EXISTS run_trades (   -- filled, immutable
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

CREATE TABLE IF NOT EXISTS run_equity (   -- sampled per /step, NOT per bar
    run_id          TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sim_time        TEXT NOT NULL,
    cash            REAL NOT NULL,
    positions_value REAL NOT NULL,
    equity          REAL NOT NULL,
    PRIMARY KEY (run_id, sim_time)
);
