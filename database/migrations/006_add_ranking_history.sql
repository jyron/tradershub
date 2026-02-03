-- Track daily bot rankings for movement indicators
CREATE TABLE IF NOT EXISTS ranking_snapshots (
    id TEXT PRIMARY KEY,
    bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    rank INTEGER NOT NULL,
    total_value REAL NOT NULL,
    snapshot_date TEXT NOT NULL,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(bot_id, snapshot_date)
);

CREATE INDEX IF NOT EXISTS idx_ranking_snapshots_bot ON ranking_snapshots(bot_id);
CREATE INDEX IF NOT EXISTS idx_ranking_snapshots_date ON ranking_snapshots(snapshot_date);
