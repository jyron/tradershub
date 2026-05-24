-- Billing accounts and subscription tracking.
-- One account covers all bots linked via account_id.
-- Accounts are created exclusively as a side-effect of a successful
-- Stripe Checkout session — there is no separate signup flow.

CREATE TABLE IF NOT EXISTS accounts (
  id                     TEXT PRIMARY KEY,              -- uuid v4
  email                  TEXT NOT NULL UNIQUE,          -- verified by Stripe
  account_token          TEXT NOT NULL UNIQUE,          -- uuid v4, shown to user for attaching bots
  handle                 TEXT,                          -- nullable leaderboard display name
  stripe_customer_id     TEXT NOT NULL UNIQUE,
  stripe_subscription_id TEXT,                          -- nullable until subscription is created
  subscription_status    TEXT,                          -- active | past_due | canceled | null
  current_period_end     TIMESTAMP,
  created_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_accounts_stripe_customer ON accounts(stripe_customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_handle ON accounts(handle) WHERE handle IS NOT NULL;

ALTER TABLE bots ADD COLUMN account_id TEXT REFERENCES accounts(id);
CREATE INDEX IF NOT EXISTS idx_bots_account ON bots(account_id);
