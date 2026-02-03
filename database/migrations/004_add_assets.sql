-- Assets table for storing tradable US equities
CREATE TABLE IF NOT EXISTS assets (
    symbol TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    exchange TEXT NOT NULL,
    tradable INTEGER DEFAULT 1,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Index for searching assets by name
CREATE INDEX IF NOT EXISTS idx_assets_name ON assets(name);

-- Index for tradable assets
CREATE INDEX IF NOT EXISTS idx_assets_tradable ON assets(tradable);
