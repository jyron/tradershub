-- Per-bot per-day counters. Guardrail enforcement reads/increments these so
-- a buggy LLM can't drain a submitter's API key or blow through Finnhub quota.
CREATE TABLE IF NOT EXISTS bot_usage_daily (
    bot_id      TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    usage_date  TEXT NOT NULL,
    trade_count INTEGER NOT NULL DEFAULT 0,
    llm_calls   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (bot_id, usage_date)
);
