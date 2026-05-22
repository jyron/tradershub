-- Raw historical bars cache, fed by the bar_ingest job from Alpaca.
-- This is the read-mostly working set: the ingester appends, the
-- scenario_provisioner reads ranges out of it into scenario_bars.
-- Re-pulling from Alpaca is cheap, so losing this table is non-fatal.
CREATE TABLE IF NOT EXISTS bars (
    symbol      TEXT NOT NULL,
    ts          TEXT NOT NULL,            -- ISO UTC, hourly aligned (e.g. 2024-03-15T14:00:00Z)
    open        REAL NOT NULL,
    high        REAL NOT NULL,
    low         REAL NOT NULL,
    close       REAL NOT NULL,
    volume      INTEGER NOT NULL,
    source      TEXT NOT NULL DEFAULT 'alpaca-iex',
    ingested_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (symbol, ts)
);

CREATE INDEX IF NOT EXISTS idx_bars_ts ON bars(ts);
