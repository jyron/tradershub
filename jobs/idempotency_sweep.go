package jobs

import (
	"bottrade/database"
	"log"
	"time"
)

// IdempotencySweepJob deletes run_idempotency rows older than 24h.
// The TTL is short on purpose: clients shouldn't be retrying the same
// key against a long-completed run, and keeping the table small lets the
// lookup stay a single primary-key hit.
type IdempotencySweepJob struct{}

func NewIdempotencySweepJob() *IdempotencySweepJob { return &IdempotencySweepJob{} }

func (j *IdempotencySweepJob) Name() string { return "IdempotencySweep" }

func (j *IdempotencySweepJob) Interval() time.Duration { return 1 * time.Hour }

func (j *IdempotencySweepJob) Run() error {
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	res, err := database.DB.Exec(
		`DELETE FROM run_idempotency WHERE created_at < ?1`,
		cutoff,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("IdempotencySweep: deleted %d row(s)", n)
	}
	return nil
}
