-- Hosted-bot submissions: submitter pastes an LLM API key, we encrypt and
-- store it so the dynamic bot runner can spawn their bot on our schedule.
-- Kept off the bots table so a stray SELECT * never leaks ciphertext.
CREATE TABLE IF NOT EXISTS bot_credentials (
    bot_id        TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    base_url      TEXT,
    model_id      TEXT NOT NULL,
    encrypted_key BLOB NOT NULL,
    nonce         BLOB NOT NULL,
    key_version   INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT DEFAULT CURRENT_TIMESTAMP,
    updated_at    TEXT DEFAULT CURRENT_TIMESTAMP
);

-- Tier ladder for the leaderboard.
--   challenger: newly submitted; appears on /models?tier=challenger
--   verified:   passed 14d / 25-trade promotion bar; appears on default board
--   official:   hand-curated frontier benchmark (the existing 4 bots + baselines)
ALTER TABLE bots ADD COLUMN tier TEXT DEFAULT 'challenger';
ALTER TABLE bots ADD COLUMN consecutive_errors INTEGER DEFAULT 0;
ALTER TABLE bots ADD COLUMN last_error TEXT;
ALTER TABLE bots ADD COLUMN last_run_at TEXT;
ALTER TABLE bots ADD COLUMN disabled_reason TEXT;

UPDATE bots SET tier = 'official' WHERE COALESCE(is_official, 0) = 1;

CREATE INDEX IF NOT EXISTS idx_bots_tier ON bots(tier);
