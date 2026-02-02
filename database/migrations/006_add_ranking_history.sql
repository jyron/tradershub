-- Track daily bot rankings for movement indicators
CREATE TABLE IF NOT EXISTS ranking_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    rank INT NOT NULL,
    total_value DECIMAL(15,2) NOT NULL,
    snapshot_date DATE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(bot_id, snapshot_date)
);

CREATE INDEX IF NOT EXISTS idx_ranking_snapshots_bot ON ranking_snapshots(bot_id);
CREATE INDEX IF NOT EXISTS idx_ranking_snapshots_date ON ranking_snapshots(snapshot_date);
