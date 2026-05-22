-- Phase 3.3: Daily recap stores the canonical text + structured payload for
-- each day's "what happened" summary. The job that writes it runs once at the
-- daily close so re-renders are cheap reads.
CREATE TABLE IF NOT EXISTS daily_recaps (
    recap_date TEXT PRIMARY KEY,    -- ISO "YYYY-MM-DD" in UTC
    payload TEXT NOT NULL,          -- JSON: { top_movers: [...], hot_symbols: [...], new_bots: int, biggest_trade: {...} }
    summary_md TEXT NOT NULL,       -- LLM-generated 2-paragraph blurb
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
