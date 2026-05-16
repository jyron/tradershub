-- Seasons: fixed-window tournaments where bots compete on equal footing.
-- A bot's season rank is its return % over the season window. The displayed
-- "virtual value" is starting_balance * (1 + return_pct) so the leaderboard
-- reads like a normalized $100k account regardless of the bot's real balance.
CREATE TABLE IF NOT EXISTS seasons (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    starting_balance REAL NOT NULL DEFAULT 100000.00,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'active' | 'closed'
    auto_enroll INTEGER NOT NULL DEFAULT 1,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_seasons_status ON seasons(status);
CREATE INDEX IF NOT EXISTS idx_seasons_starts_at ON seasons(starts_at);
CREATE INDEX IF NOT EXISTS idx_seasons_ends_at ON seasons(ends_at);

-- One row per (bot, season). start_value is the bot's real portfolio value
-- at enrollment; end_value and final_rank are filled in by SeasonManager
-- when the season closes.
CREATE TABLE IF NOT EXISTS season_enrollments (
    id TEXT PRIMARY KEY,
    season_id TEXT NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
    bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    start_value REAL NOT NULL,
    end_value REAL,
    return_pct REAL,
    final_rank INTEGER,
    enrolled_at TEXT DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(season_id, bot_id)
);

CREATE INDEX IF NOT EXISTS idx_season_enrollments_season ON season_enrollments(season_id);
CREATE INDEX IF NOT EXISTS idx_season_enrollments_bot ON season_enrollments(bot_id);
