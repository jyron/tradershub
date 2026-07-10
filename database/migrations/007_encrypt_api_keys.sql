-- API keys are now encrypted at rest and looked up by hash.
--
-- api_key       — was the plaintext key; now holds AES-256-GCM ciphertext
--                 (key from APP_ENCRYPTION_KEY, held in the environment, not
--                 the DB). A leaked DB dump exposes no usable credentials.
-- api_key_hash  — SHA-256 of the plaintext key; the lookup index used by auth.
--
-- Existing rows are migrated in-place on first boot by the Go one-time backfill
-- BackfillAPIKeyEncryption (SQLite has no sha256/encrypt function). That
-- backfill is idempotent and guarded on api_key_hash IS NULL.
ALTER TABLE api_keys ADD COLUMN api_key_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(api_key_hash);
