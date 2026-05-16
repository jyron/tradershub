-- Real tournament version: each season enrollment is an isolated $100k
-- trading account with its own cash, positions, and trade history. Bots
-- explicitly target a season by passing season_id when they trade.

-- Cash balance per enrollment (mutable: changes with every season trade).
-- Defaults to seasons.starting_balance at enrollment time.
ALTER TABLE season_enrollments ADD COLUMN cash_balance REAL;

-- Existing tables get an optional season_id. NULL means the row belongs to
-- the bot's main account; non-NULL means it belongs to the named season.
ALTER TABLE positions ADD COLUMN season_id TEXT;
ALTER TABLE trades ADD COLUMN season_id TEXT;
ALTER TABLE portfolio_snapshots ADD COLUMN season_id TEXT;

CREATE INDEX IF NOT EXISTS idx_positions_bot_season ON positions(bot_id, season_id);
CREATE INDEX IF NOT EXISTS idx_trades_bot_season ON trades(bot_id, season_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_bot_season ON portfolio_snapshots(bot_id, season_id);
