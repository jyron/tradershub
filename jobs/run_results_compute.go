package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"log"
	"time"
)

// RunResultsComputeJob catches up on any completed/liquidated/abandoned
// runs that don't yet have a run_results row. The /results endpoint also
// computes synchronously on first request, so this job is mainly a safety
// net for the case where an agent finishes a run and never calls /results.
type RunResultsComputeJob struct {
	engine *services.ScenarioEngine
}

func NewRunResultsComputeJob(engine *services.ScenarioEngine) *RunResultsComputeJob {
	return &RunResultsComputeJob{engine: engine}
}

func (j *RunResultsComputeJob) Name() string { return "RunResultsCompute" }

func (j *RunResultsComputeJob) Interval() time.Duration { return 5 * time.Minute }

func (j *RunResultsComputeJob) Run() error {
	if j.engine == nil {
		return nil
	}
	rows, err := database.DB.Query(`
		SELECT r.id
		  FROM runs r
		  LEFT JOIN run_results rr ON rr.run_id = r.id
		 WHERE r.status IN ('completed','liquidated','abandoned')
		   AND rr.run_id IS NULL
		 LIMIT 100
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if _, err := j.engine.ComputeResults(id); err != nil {
			log.Printf("RunResultsCompute: %s: %v", id, err)
			continue
		}
		count++
	}
	if count > 0 {
		log.Printf("RunResultsCompute: computed %d run result(s)", count)
	}
	return nil
}
