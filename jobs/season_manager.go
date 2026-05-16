package jobs

import (
	"bottrade/services"
	"log"
	"time"
)

// SeasonManagerJob advances every season through its lifecycle. It starts
// pending seasons whose starts_at has passed (auto-enrolling all active bots
// for auto_enroll=1 seasons) and closes active seasons whose ends_at has
// passed (capturing each enrollment's end value and final rank).
type SeasonManagerJob struct {
	seasons *services.SeasonService
}

func NewSeasonManagerJob() *SeasonManagerJob {
	return &SeasonManagerJob{seasons: services.NewSeasonService()}
}

func (j *SeasonManagerJob) Name() string {
	return "SeasonManager"
}

// Interval is 5 minutes. Seasons run for days or weeks, so finer granularity
// would just burn DB cycles; coarser would let a season hang in "pending"
// after its start time and confuse users.
func (j *SeasonManagerJob) Interval() time.Duration {
	return 5 * time.Minute
}

func (j *SeasonManagerJob) Run() error {
	now := time.Now().UTC()

	startable, err := j.seasons.FindStartableSeasons(now)
	if err != nil {
		log.Printf("SeasonManager: find startable: %v", err)
	}
	for _, id := range startable {
		if err := j.seasons.StartSeason(id); err != nil {
			log.Printf("SeasonManager: start %s: %v", id, err)
			continue
		}
		log.Printf("SeasonManager: started season %s", id)
	}

	closable, err := j.seasons.FindClosableSeasons(now)
	if err != nil {
		log.Printf("SeasonManager: find closable: %v", err)
	}
	for _, id := range closable {
		if err := j.seasons.CloseSeason(id); err != nil {
			log.Printf("SeasonManager: close %s: %v", id, err)
			continue
		}
		log.Printf("SeasonManager: closed season %s", id)
	}
	return nil
}
