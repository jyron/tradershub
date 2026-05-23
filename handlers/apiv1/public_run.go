package apiv1

import (
	"bottrade/database"
	"database/sql"

	"github.com/gofiber/fiber/v2"
)

// mountPublicRun registers GET /v1/runs/{id}/public — a read-only view of
// a run that has been published to the public leaderboard. Mounted outside
// huma so it works for visitors browsing the leaderboard without an
// X-API-Key. Returns 404 for unpublished runs (don't leak existence).
func (h *handlers) mountPublicRun(app *fiber.App) {
	app.Get("/v1/runs/:id/public", h.getPublicRun)
}

func (h *handlers) getPublicRun(c *fiber.Ctx) error {
	runID := c.Params("id")

	var published int
	if err := database.DB.QueryRow(
		`SELECT published FROM runs WHERE id = ?1`, runID,
	).Scan(&published); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no such run"})
	}
	if published == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no such run"})
	}

	snap, err := h.Engine.GetRunState(runID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no such run"})
	}

	var (
		finalEquity  float64
		returnPct    float64
		sharpe       sql.NullFloat64
		sortino      sql.NullFloat64
		maxDrawdown  sql.NullFloat64
		volatility   sql.NullFloat64
		tradeCount   int
		liquidated   int
		hasResults   bool
	)
	if err := database.DB.QueryRow(
		`SELECT final_equity, return_pct, sharpe, sortino, max_drawdown,
		        volatility, trade_count, liquidated
		   FROM run_results WHERE run_id = ?1`,
		runID,
	).Scan(&finalEquity, &returnPct, &sharpe, &sortino, &maxDrawdown,
		&volatility, &tradeCount, &liquidated); err == nil {
		hasResults = true
	}

	resp := fiber.Map{
		"run":           snap.Run,
		"positions":     snap.Positions,
		"queued_orders": snap.QueuedOrders,
		"last_equity":   snap.LastEquity,
	}
	if hasResults {
		resp["results"] = fiber.Map{
			"final_equity": finalEquity,
			"return_pct":   returnPct,
			"sharpe":       nullFloat(sharpe),
			"sortino":      nullFloat(sortino),
			"max_drawdown": nullFloat(maxDrawdown),
			"volatility":   nullFloat(volatility),
			"trade_count":  tradeCount,
			"liquidated":   liquidated != 0,
		}
	}
	return c.JSON(resp)
}

func nullFloat(n sql.NullFloat64) interface{} {
	if !n.Valid {
		return nil
	}
	return n.Float64
}
