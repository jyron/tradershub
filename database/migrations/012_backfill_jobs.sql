-- Async work queue for 30-day backfill replays of newly submitted bots.
-- The backfill_runner job polls for 'queued' rows, spawns the python
-- replay, and streams progress via days_done.
CREATE TABLE IF NOT EXISTS backfill_jobs (
    id              TEXT PRIMARY KEY,
    bot_id          TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'queued',
    days_requested  INTEGER NOT NULL,
    days_done       INTEGER NOT NULL DEFAULT 0,
    error           TEXT,
    log_tail        TEXT,
    requested_at    TEXT DEFAULT CURRENT_TIMESTAMP,
    started_at      TEXT,
    completed_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_backfill_jobs_status ON backfill_jobs(status);
CREATE INDEX IF NOT EXISTS idx_backfill_jobs_bot ON backfill_jobs(bot_id);
