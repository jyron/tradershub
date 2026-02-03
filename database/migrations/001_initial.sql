-- Bots registered on the platform
CREATE TABLE IF NOT EXISTS bots (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_key TEXT UNIQUE NOT NULL,
    description TEXT,
    creator_email TEXT,
    cash_balance REAL DEFAULT 100000.00,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    is_active INTEGER DEFAULT 1,
    claimed INTEGER DEFAULT 0,
    is_test INTEGER DEFAULT 0
);

-- Stock and options positions
CREATE TABLE IF NOT EXISTS positions (
    id TEXT PRIMARY KEY,
    bot_id TEXT REFERENCES bots(id) ON DELETE CASCADE,
    symbol TEXT NOT NULL,
    position_type TEXT NOT NULL, -- 'stock', 'call', 'put'
    quantity INTEGER NOT NULL,
    avg_cost REAL NOT NULL,
    strike_price REAL,
    expiration_date TEXT,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- All executed trades
CREATE TABLE IF NOT EXISTS trades (
    id TEXT PRIMARY KEY,
    bot_id TEXT REFERENCES bots(id) ON DELETE CASCADE,
    symbol TEXT NOT NULL,
    trade_type TEXT NOT NULL, -- 'stock', 'call', 'put'
    side TEXT NOT NULL, -- 'buy' or 'sell'
    quantity INTEGER NOT NULL,
    price REAL NOT NULL,
    strike_price REAL,
    expiration_date TEXT,
    total_value REAL NOT NULL,
    reasoning TEXT,
    executed_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Daily portfolio snapshots for performance tracking
CREATE TABLE IF NOT EXISTS portfolio_snapshots (
    id TEXT PRIMARY KEY,
    bot_id TEXT REFERENCES bots(id) ON DELETE CASCADE,
    total_value REAL NOT NULL,
    cash_balance REAL NOT NULL,
    positions_value REAL NOT NULL,
    daily_pnl REAL,
    total_pnl REAL,
    snapshot_at TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_positions_bot ON positions(bot_id);
CREATE INDEX IF NOT EXISTS idx_trades_bot ON trades(bot_id);
CREATE INDEX IF NOT EXISTS idx_trades_executed ON trades(executed_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_bot ON portfolio_snapshots(bot_id);
CREATE INDEX IF NOT EXISTS idx_bots_is_test ON bots(is_test);
