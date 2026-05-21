-- is_baseline marks synthetic reference bots (random walker, buy-and-hold SPY,
-- equal-weight rebalancer). They run as tier='official' so they show on the
-- main board, but the UI pins them to a separate "baselines" row to make it
-- clear they're a reference line, not a competitor.
ALTER TABLE bots ADD COLUMN is_baseline INTEGER DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_bots_is_baseline ON bots(is_baseline) WHERE is_baseline = 1;
