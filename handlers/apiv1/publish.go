package apiv1

import (
	"github.com/gofiber/fiber/v2"
)

// Publish pushes the run's results to the public per-scenario leaderboard.
// Idempotent — re-publishes update the existing row.
//   POST /v1/runs/:id/publish
func (h *Handlers) Publish(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}

	// Ensure results are computed; if active, this errors.
	results, err := h.Engine.ComputeResults(runID)
	if err != nil {
		return jsonErrorf(c, fiber.StatusBadRequest, "publish_failed", "%v", err)
	}

	// Fetch scenario_id + bot_id off the run.
	var scenarioID, botID string
	if err := h.Engine.AppDB().QueryRow(`SELECT scenario_id, bot_id FROM runs WHERE id = ?1`, runID).Scan(&scenarioID, &botID); err != nil {
		return jsonErrorf(c, fiber.StatusInternalServerError, "db_error", "%v", err)
	}

	var sharpe interface{}
	if results.Sharpe != nil {
		sharpe = *results.Sharpe
	}
	if _, err := h.Engine.AppDB().Exec(`
		INSERT INTO run_leaderboard (scenario_id, run_id, bot_id, return_pct, sharpe)
		VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT (scenario_id, run_id) DO UPDATE SET
			return_pct = excluded.return_pct,
			sharpe = excluded.sharpe,
			published_at = CURRENT_TIMESTAMP
	`, scenarioID, runID, botID, results.ReturnPct, sharpe); err != nil {
		return jsonErrorf(c, fiber.StatusInternalServerError, "leaderboard_insert_failed", "%v", err)
	}
	if _, err := h.Engine.AppDB().Exec(`UPDATE runs SET published = 1 WHERE id = ?1`, runID); err != nil {
		return jsonErrorf(c, fiber.StatusInternalServerError, "db_error", "%v", err)
	}
	return c.JSON(fiber.Map{"published": true, "results": results})
}
