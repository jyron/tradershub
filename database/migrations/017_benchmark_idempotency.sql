-- Benchmark API: per-run idempotency table.
-- Agents in tight loops retry on network blips; without dedup a single
-- POST /trades retry double-fills. Same (run_id, key) + matching
-- request_hash → cached response; mismatched hash → 409 conflict.
--
-- Rows are swept after 24h by the idempotency_sweep job; clients can't
-- replay an old key against a new state.
CREATE TABLE IF NOT EXISTS run_idempotency (
    run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    request_hash  TEXT NOT NULL,           -- sha256 of canonical request body
    response_json TEXT NOT NULL,
    status_code   INTEGER NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_created ON run_idempotency(created_at);
