-- Bots registered on the platform
CREATE TABLE IF NOT EXISTS bots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    api_key VARCHAR(64) UNIQUE NOT NULL,
    description TEXT,
    creator_email VARCHAR(255),
    cash_balance DECIMAL(15,2) DEFAULT 100000.00,
    created_at TIMESTAMP DEFAULT NOW(),
    is_active BOOLEAN DEFAULT true
);

-- Stock and options positions
CREATE TABLE IF NOT EXISTS positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID REFERENCES bots(id) ON DELETE CASCADE,
    symbol VARCHAR(10) NOT NULL,
    position_type VARCHAR(20) NOT NULL, -- 'stock', 'call', 'put'
    quantity INTEGER NOT NULL,
    avg_cost DECIMAL(15,4) NOT NULL,
    strike_price DECIMAL(15,2),
    expiration_date DATE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- All executed trades
CREATE TABLE IF NOT EXISTS trades (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID REFERENCES bots(id) ON DELETE CASCADE,
    symbol VARCHAR(10) NOT NULL,
    trade_type VARCHAR(20) NOT NULL, -- 'stock', 'call', 'put'
    side VARCHAR(4) NOT NULL, -- 'buy' or 'sell'
    quantity INTEGER NOT NULL,
    price DECIMAL(15,4) NOT NULL,
    strike_price DECIMAL(15,2),
    expiration_date DATE,
    total_value DECIMAL(15,2) NOT NULL,
    reasoning TEXT,
    executed_at TIMESTAMP DEFAULT NOW()
);

-- Daily portfolio snapshots for performance tracking
CREATE TABLE IF NOT EXISTS portfolio_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID REFERENCES bots(id) ON DELETE CASCADE,
    total_value DECIMAL(15,2) NOT NULL,
    cash_balance DECIMAL(15,2) NOT NULL,
    positions_value DECIMAL(15,2) NOT NULL,
    daily_pnl DECIMAL(15,2),
    total_pnl DECIMAL(15,2),
    snapshot_at TIMESTAMP DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_positions_bot ON positions(bot_id);
CREATE INDEX IF NOT EXISTS idx_trades_bot ON trades(bot_id);
CREATE INDEX IF NOT EXISTS idx_trades_executed ON trades(executed_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_bot ON portfolio_snapshots(bot_id);
