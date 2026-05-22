package apiv1

import (
	"bottrade/database"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// idempotencyEntry is the persisted record of a prior request response.
type idempotencyEntry struct {
	RequestHash  string
	ResponseJSON string
	StatusCode   int
}

// hashRequest produces the canonical sha256 of any input struct we'd cache.
// Because huma parses the request body into the typed struct BEFORE the
// handler runs, we don't have the raw bytes. We re-marshal the body
// portion of the input — Go's encoding/json sorts map keys alphabetically
// and writes structs in field-declaration order, so the same input
// always produces the same hash.
func hashRequest(v interface{}) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// lookupIdempotency returns the cached entry for (runID, key) if any.
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

// idempotent wraps the actual operation logic. If idempKey is set and the
// run has a previously-stored response for that key, returns the cached
// response (after verifying the request body matches). Mismatched body
// for the same key returns 409 via huma.
//
// On a fresh request, runs `do()` and persists the response.
//
// The returned interface is whatever `do` returned (one of the handler
// output types). Returns true when the response came from cache.
func idempotent[Out any](
	runID, idempKey string,
	requestForHash interface{},
	do func() (*Out, error),
) (*Out, error) {
	if idempKey == "" {
		return do()
	}
	hash := hashRequest(requestForHash)
	prior, err := lookupIdempotency(runID, idempKey)
	if err != nil {
		return nil, huma.Error500InternalServerError("idempotency lookup failed: " + err.Error())
	}
	if prior != nil {
		if prior.RequestHash != hash {
			return nil, huma.NewError(http.StatusConflict,
				"idempotency_key previously used with a different request body")
		}
		var cached Out
		if err := json.Unmarshal([]byte(prior.ResponseJSON), &cached); err != nil {
			return nil, huma.Error500InternalServerError("could not decode cached response: " + err.Error())
		}
		return &cached, nil
	}

	out, err := do()
	if err != nil {
		return nil, err
	}
	respBytes, mErr := json.Marshal(out)
	if mErr != nil {
		return nil, huma.Error500InternalServerError("could not encode response: " + mErr.Error())
	}
	if err := storeIdempotency(runID, idempKey, hash, string(respBytes), http.StatusOK); err != nil {
		return nil, huma.Error500InternalServerError("could not store idempotency record: " + err.Error())
	}
	return out, nil
}
