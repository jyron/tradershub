package jobs

import (
	"bottrade/database"
	"log"
	"time"
)

// IdleRunCleanupJob marks any run that hasn't had activity in
// idleTimeout as abandoned. Activity = the last_activity_at column,
// which the engine updates on QueueTrade and AdvanceStep.
//
// Abandoned runs are kept in the DB so their trade log and equity curve
// remain available; the status just blocks any further /trade or /step.
type IdleRunCleanupJob struct{}

func NewIdleRunCleanupJob() *IdleRunCleanupJob { return &IdleRunCleanupJob{} }

func (j *IdleRunCleanupJob) Name() string { return "IdleRunCleanup" }

func (j *IdleRunCleanupJob) Interval() time.Duration { return 1 * time.Hour }

// idleTimeout is the cutoff: 5 days of inactivity → abandoned.
const idleTimeout = 5 * 24 * time.Hour

func (j *IdleRunCleanupJob) Run() error {
	cutoff := time.Now().UTC().Add(-idleTimeout).Format(time.RFC3339)
	res, err := database.DB.Exec(`
		UPDATE runs
		   SET status = 'abandoned', completed_at = CURRENT_TIMESTAMP
		 WHERE status = 'active'
		   AND last_activity_at < ?1
	`, cutoff)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("IdleRunCleanup: marked %d run(s) abandoned (idle > 5d)", n)
	}
	return nil
}
