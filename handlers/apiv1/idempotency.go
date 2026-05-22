package apiv1

import (
	"bottrade/database"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// idempotencyEntry is what's persisted in run_idempotency.
type idempotencyEntry struct {
	RequestHash  string
	ResponseJSON string
	StatusCode   int
}

// hashRequest returns the sha256 hex of the canonical (sorted-keys) JSON
// representation of body — same body → same hash, regardless of map ordering.
func hashRequest(body []byte) string {
	// Re-encode to canonical form (compact + sorted keys via json.Marshal-of-map-roundtrip).
	var v interface{}
	if err := json.Unmarshal(body, &v); err == nil {
		if canonical, err := json.Marshal(v); err == nil {
			h := sha256.Sum256(canonical)
			return hex.EncodeToString(h[:])
		}
	}
	// Fallback: hash raw bytes
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// lookupIdempotency returns a cached response for (runID, key) if one
// exists. Returns nil,nil if no row. If the row exists but request_hash
// doesn't match the new body, returns the entry with the *existing* hash
// so the caller can 409.
func lookupIdempotency(runID, key string) (*idempotencyEntry, error) {
	if key == "" {
		return nil, nil
	}
	row := database.DB.QueryRow(`
		SELECT request_hash, response_json, status_code
		  FROM run_idempotency WHERE run_id = ?1 AND key = ?2
	`, runID, key)
	var e idempotencyEntry
	if err := row.Scan(&e.RequestHash, &e.ResponseJSON, &e.StatusCode); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// storeIdempotency inserts the (runID, key) → response row.
func storeIdempotency(runID, key, requestHash, responseJSON string, statusCode int) error {
	if key == "" {
		return nil
	}
	_, err := database.DB.Exec(`
		INSERT INTO run_idempotency (run_id, key, request_hash, response_json, status_code)
		VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT (run_id, key) DO NOTHING
	`, runID, key, requestHash, responseJSON, statusCode)
	return err
}

// withIdempotency wraps a handler body. If an idempotency_key is present
// and matches a prior request, replays the cached response. If it matches
// a prior key with a different body, returns 409. Otherwise runs `do()`
// and stores the result.
//
// do() must return (statusCode, responseBody) and a non-nil error short-circuits.
func withIdempotency(c *fiber.Ctx, runID, key string, do func() (int, interface{}, error)) error {
	bodyBytes := c.Body() // already-read body; Fiber buffers it

	if key != "" {
		hash := hashRequest(bodyBytes)
		prior, err := lookupIdempotency(runID, key)
		if err != nil {
			return jsonErrorf(c, fiber.StatusInternalServerError, "idempotency_lookup_failed", "%v", err)
		}
		if prior != nil {
			if prior.RequestHash != hash {
				return jsonError(c, fiber.StatusConflict, "idempotency_key_reused",
					"idempotency_key was previously used with a different request body")
			}
			// Replay cached response.
			c.Status(prior.StatusCode)
			c.Set("Content-Type", "application/json")
			return c.SendString(prior.ResponseJSON)
		}
	}

	status, body, err := do()
	if err != nil {
		return err
	}

	// Serialize the response so we can both return it AND cache it.
	respBytes, mErr := json.Marshal(body)
	if mErr != nil {
		return jsonErrorf(c, fiber.StatusInternalServerError, "marshal_failed", "%v", mErr)
	}

	if key != "" {
		if err := storeIdempotency(runID, key, hashRequest(bodyBytes), string(respBytes), status); err != nil {
			return jsonErrorf(c, fiber.StatusInternalServerError, "idempotency_store_failed", "%v", err)
		}
	}

	c.Status(status)
	c.Set("Content-Type", "application/json")
	return c.SendString(string(respBytes))
}
