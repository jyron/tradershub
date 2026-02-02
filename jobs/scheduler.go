package jobs

import (
	"log"
	"sync"
	"time"
)

type Job interface {
	Run() error
	Name() string
}

type Scheduler struct {
	jobs    []Job
	mu      sync.Mutex
	running bool
	done    chan bool
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs: make([]Job, 0),
		done: make(chan bool),
	}
}

func (s *Scheduler) AddJob(job Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job)
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	log.Println("Background job scheduler started")

	for _, job := range s.jobs {
		go s.runJobPeriodically(job)
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.done)
	log.Println("Background job scheduler stopped")
}

func (s *Scheduler) runJobPeriodically(job Job) {
	log.Printf("Starting background job: %s", job.Name())

	if err := job.Run(); err != nil {
		log.Printf("Error running job %s: %v", job.Name(), err)
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("Running scheduled job: %s", job.Name())
			if err := job.Run(); err != nil {
				log.Printf("Error running job %s: %v", job.Name(), err)
			}
		case <-s.done:
			log.Printf("Stopping job: %s", job.Name())
			return
		}
	}
}
