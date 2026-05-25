-- API keys are the billing and quota principal.
-- Existing bots are owner-created keys and should keep Pro access forever.

CREATE TABLE api_keys (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    api_key                TEXT UNIQUE NOT NULL,
    description            TEXT,
    creator_email          TEXT,
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

INSERT INTO api_keys (
    id, name, api_key, description, creator_email, created_at,
    is_active, disabled_reason, plan
)
SELECT
    id, name, api_key, description, creator_email, created_at,
    is_active, disabled_reason, 'pro'
FROM bots;

CREATE INDEX idx_api_keys_api_key ON api_keys(api_key);
CREATE INDEX idx_api_keys_stripe_customer ON api_keys(stripe_customer_id);
CREATE UNIQUE INDEX idx_api_keys_handle ON api_keys(handle) WHERE handle IS NOT NULL;

CREATE TABLE runs_new (
    id               TEXT PRIMARY KEY,
    api_key_id       TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    bot_name         TEXT,
    scenario_id      TEXT NOT NULL REFERENCES scenarios(id),
    scenario_version INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    sim_time         TEXT NOT NULL,
    cash             REAL NOT NULL,
    starting_cash    REAL NOT NULL,
    last_activity_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at     TEXT,
    published        INTEGER NOT NULL DEFAULT 0
);

INSERT INTO runs_new (
    id, api_key_id, bot_name, scenario_id, scenario_version, status,
    sim_time, cash, starting_cash, last_activity_at, created_at,
    completed_at, published
)
SELECT
    r.id, r.bot_id, b.name, r.scenario_id, r.scenario_version, r.status,
    r.sim_time, r.cash, r.starting_cash, r.last_activity_at, r.created_at,
    r.completed_at, r.published
FROM runs r
LEFT JOIN bots b ON b.id = r.bot_id;

DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE INDEX idx_runs_api_key ON runs(api_key_id);
CREATE INDEX idx_runs_status_activity ON runs(status, last_activity_at);

CREATE TABLE run_leaderboard_new (
    scenario_id  TEXT NOT NULL REFERENCES scenarios(id),
    run_id       TEXT NOT NULL REFERENCES runs(id),
    api_key_id   TEXT NOT NULL REFERENCES api_keys(id),
    bot_name     TEXT,
    return_pct   REAL NOT NULL,
    sharpe       REAL,
    published_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (scenario_id, run_id)
);

INSERT INTO run_leaderboard_new (
    scenario_id, run_id, api_key_id, bot_name, return_pct, sharpe, published_at
)
SELECT
    l.scenario_id, l.run_id, l.bot_id, COALESCE(r.bot_name, b.name),
    l.return_pct, l.sharpe, l.published_at
FROM run_leaderboard l
LEFT JOIN runs r ON r.id = l.run_id
LEFT JOIN bots b ON b.id = l.bot_id;

DROP TABLE run_leaderboard;
ALTER TABLE run_leaderboard_new RENAME TO run_leaderboard;
CREATE INDEX idx_leaderboard_scenario
    ON run_leaderboard(scenario_id, return_pct DESC);

DROP TABLE IF EXISTS accounts;
DROP TABLE bots;
