-- Transactional-email dedupe ledger. One row per (account, kind, period);
-- INSERT OR IGNORE before sending means an email of a given kind goes out at
-- most once per period. Welcome emails use period '' (once ever); quota
-- emails use the UTC 'YYYY-MM' month.
CREATE TABLE IF NOT EXISTS email_log (
    account_id TEXT NOT NULL,
    kind       TEXT NOT NULL,
    period     TEXT NOT NULL DEFAULT '',
    sent_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, kind, period)
);
