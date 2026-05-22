-- Immutable frozen-per-scenario-version copy of bars used by the simulator.
-- A scenario is provisioned once by copying the relevant slice of `bars`
-- into here, then the scenario can be replayed against forever without
-- worrying about the raw bars table being mutated or re-pulled.
--
-- Composite PK ensures one row per (scenario, version, symbol, bar).
-- slippage_bps is frozen INTO the bar so a future change to the scenario
-- definition can't retroactively alter old runs' fill prices.
CREATE TABLE IF NOT EXISTS scenario_bars (
    scenario_id      TEXT NOT NULL,
    scenario_version INTEGER NOT NULL,
    symbol           TEXT NOT NULL,
    ts               TEXT NOT NULL,
    open             REAL NOT NULL,
    high             REAL NOT NULL,
    low              REAL NOT NULL,
    close            REAL NOT NULL,
    volume           INTEGER NOT NULL,
    slippage_bps     INTEGER NOT NULL,
    PRIMARY KEY (scenario_id, scenario_version, symbol, ts)
);

CREATE INDEX IF NOT EXISTS idx_scenario_bars_ts
    ON scenario_bars(scenario_id, scenario_version, ts);
