-- Adds the model_provider field (claude/gpt/gemini/grok/meta/other) so the UI
-- can render model chips and group bots by provider on the showdown page.
--
-- is_official marks the canonical benchmark bots that the showdown page is
-- built around. Official bots are exempt from cleanup scripts so the demo
-- data is always present.
--
-- model_provider is intentionally nullable: legacy bots registered before this
-- migration simply don't render a chip. New registrations *should* set it,
-- but the field is not required.

ALTER TABLE bots ADD COLUMN model_provider TEXT;
ALTER TABLE bots ADD COLUMN is_official INTEGER DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_bots_model_provider ON bots(model_provider);
CREATE INDEX IF NOT EXISTS idx_bots_is_official ON bots(is_official) WHERE is_official = 1;
