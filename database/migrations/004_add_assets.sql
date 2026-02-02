-- Assets table for storing tradable US equities
CREATE TABLE IF NOT EXISTS assets (
    symbol VARCHAR(10) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    exchange VARCHAR(50) NOT NULL,
    tradable BOOLEAN DEFAULT true,
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Index for searching assets by name
CREATE INDEX IF NOT EXISTS idx_assets_name ON assets(name);

-- Index for tradable assets
CREATE INDEX IF NOT EXISTS idx_assets_tradable ON assets(tradable);
