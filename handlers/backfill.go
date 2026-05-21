package handlers

import (
	"bottrade/database"
	"database/sql"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type backfillStatus struct {
	JobID         string  `json:"job_id"`
	BotID         string  `json:"bot_id"`
	Status        string  `json:"status"`
	DaysRequested int     `json:"days_requested"`
	DaysDone      int     `json:"days_done"`
	Error         *string `json:"error,omitempty"`
	LogTail       *string `json:"log_tail,omitempty"`
	RequestedAt   string  `json:"requested_at,omitempty"`
	StartedAt     *string `json:"started_at,omitempty"`
	CompletedAt   *string `json:"completed_at,omitempty"`
}

func scanBackfill(row *sql.Row) (*backfillStatus, error) {
	var s backfillStatus
	var errStr, logTail, startedAt, completedAt sql.NullString
	err := row.Scan(
		&s.JobID, &s.BotID, &s.Status, &s.DaysRequested, &s.DaysDone,
		&errStr, &logTail, &s.RequestedAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	if errStr.Valid {
		s.Error = &errStr.String
	}
	if logTail.Valid {
		s.LogTail = &logTail.String
	}
	if startedAt.Valid {
		s.StartedAt = &startedAt.String
	}
	if completedAt.Valid {
		s.CompletedAt = &completedAt.String
	}
	return &s, nil
}

// GetBackfillJob — GET /api/backfill/:job_id
func GetBackfillJob(c *fiber.Ctx) error {
	jobID := c.Params("job_id")
	if _, err := uuid.Parse(jobID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid job id"})
	}
	row := database.DB.QueryRow(
		`SELECT id, bot_id, status, days_requested, days_done,
		        error, log_tail, requested_at, started_at, completed_at
		   FROM backfill_jobs WHERE id = ?`,
		jobID,
	)
	s, err := scanBackfill(row)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Backfill job not found"})
	}
	return c.JSON(s)
}

// GetBotBackfill — GET /api/bots/:bot_id/backfill (latest job for that bot,
// so the submission UI can poll without remembering the job id).
func GetBotBackfill(c *fiber.Ctx) error {
	botIDStr := c.Params("bot_id")
	if _, err := uuid.Parse(botIDStr); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid bot id"})
	}
	row := database.DB.QueryRow(
		`SELECT id, bot_id, status, days_requested, days_done,
		        error, log_tail, requested_at, started_at, completed_at
		   FROM backfill_jobs
		  WHERE bot_id = ?
		  ORDER BY requested_at DESC
		  LIMIT 1`,
		botIDStr,
	)
	s, err := scanBackfill(row)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "No backfill job for this bot"})
	}
	return c.JSON(s)
}
