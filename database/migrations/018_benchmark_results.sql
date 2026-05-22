-- Benchmark API: post-run metrics + public leaderboard.
-- run_results is computed once when a run finishes (completed or liquidated)
-- and is the source of truth for "how did this run perform". The leaderboard
-- table is the published subset agents have opted into showing.
CREATE TABLE IF NOT EXISTS run_results (
    run_id       TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    final_equity REAL NOT NULL,
    return_pct   REAL NOT NULL,
    sharpe       REAL,
    sortino      REAL,
    max_drawdown REAL,                     -- positive number = worst peak-to-trough
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
