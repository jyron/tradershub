-- Accounts own billing, quotas, runs, and public identity.
-- API keys are BotTrade credentials that authenticate to an account.

CREATE TABLE IF NOT EXISTS accounts (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    email                  TEXT,
    created_at             TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_active              INTEGER NOT NULL DEFAULT 1,
    disabled_reason        TEXT,
    plan                   TEXT NOT NULL DEFAULT 'free',
    stripe_customer_id     TEXT UNIQUE,
    stripe_subscription_id TEXT,
    subscription_status    TEXT,
    current_period_end     TEXT,
    billing_email          TEXT,
    handle                 TEXT
);

INSERT OR IGNORE INTO accounts (
    id, name, email, created_at, is_active, disabled_reason, plan,
    stripe_customer_id, stripe_subscription_id, subscription_status,
    current_period_end, billing_email, handle
)
SELECT
    id, name, creator_email, created_at, is_active, disabled_reason, plan,
    stripe_customer_id, stripe_subscription_id, subscription_status,
    current_period_end, billing_email, handle
FROM api_keys;

ALTER TABLE api_keys ADD COLUMN account_id TEXT REFERENCES accounts(id);

UPDATE api_keys
   SET account_id = id
 WHERE account_id IS NULL OR account_id = '';

CREATE INDEX IF NOT EXISTS idx_api_keys_account ON api_keys(account_id);
CREATE INDEX IF NOT EXISTS idx_accounts_stripe_customer ON accounts(stripe_customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_handle ON accounts(handle) WHERE handle IS NOT NULL;

CREATE TABLE IF NOT EXISTS usage_events (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    credential_id TEXT REFERENCES api_keys(id) ON DELETE SET NULL,
    client_id     TEXT,
    surface       TEXT NOT NULL,
    action        TEXT NOT NULL,
    method        TEXT,
    run_id        TEXT,
    scenario_id   TEXT,
    status        TEXT,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_usage_events_account_created
    ON usage_events(account_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_events_credential_created
    ON usage_events(credential_id, created_at);

CREATE TABLE IF NOT EXISTS account_identities (
    account_id       TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    email            TEXT,
    name             TEXT,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (provider, provider_user_id)
);

CREATE INDEX IF NOT EXISTS idx_account_identities_account
    ON account_identities(account_id);

CREATE TABLE IF NOT EXISTS oauth_clients (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    redirect_uris TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS oauth_auth_requests (
    id                    TEXT PRIMARY KEY,
    client_id             TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    redirect_uri          TEXT NOT NULL,
    state                 TEXT,
    scope                 TEXT,
    resource              TEXT,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL,
    created_at            TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at            TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_auth_codes (
    code_hash             TEXT PRIMARY KEY,
    account_id            TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    client_id             TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    redirect_uri          TEXT NOT NULL,
    scope                 TEXT,
    resource              TEXT,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL,
    created_at            TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at            TEXT NOT NULL,
    used_at               TEXT
);

CREATE TABLE IF NOT EXISTS oauth_access_tokens (
    token_hash  TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    client_id   TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    scope       TEXT,
    resource    TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TEXT NOT NULL,
    revoked_at  TEXT
);

CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
    token_hash  TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    client_id   TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    scope       TEXT,
    resource    TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TEXT NOT NULL,
    revoked_at  TEXT
);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_account
    ON oauth_access_tokens(account_id, expires_at);

CREATE TABLE IF NOT EXISTS account_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_account_sessions_account
    ON account_sessions(account_id, expires_at);
