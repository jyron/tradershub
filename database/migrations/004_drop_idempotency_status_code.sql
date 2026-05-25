DROP TABLE IF EXISTS run_idempotency_new;

CREATE TABLE run_idempotency_new (
    run_id        TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (run_id, key)
);

INSERT INTO run_idempotency_new (
    run_id, key, request_hash, response_json, created_at
)
SELECT
    i.run_id, i.key, i.request_hash, i.response_json, i.created_at
FROM run_idempotency i
JOIN runs r ON r.id = i.run_id;

DROP TABLE run_idempotency;
ALTER TABLE run_idempotency_new RENAME TO run_idempotency;

CREATE INDEX idx_idempotency_created ON run_idempotency(created_at);
