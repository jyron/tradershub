package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"time"

	"github.com/gofiber/fiber/v2"
)

// mountPublicRun registers GET /api/v1/runs/{id}/public — a read-only view of
// a run that has been published to the public leaderboard. Mounted outside
// huma so it works for visitors browsing the leaderboard without an
// X-API-Key. Returns 404 for unpublished runs (don't leak existence).
func (h *handlers) mountPublicRun(app *fiber.App) {
	app.Get("/api/v1/runs/:id/public", h.getPublicRun)
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
		finalEquity float64
		returnPct   float64
		sharpe      sql.NullFloat64
		sortino     sql.NullFloat64
		maxDrawdown sql.NullFloat64
		volatility  sql.NullFloat64
		tradeCount  int
		liquidated  int
		hasResults  bool
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
	trades, err := loadPublicTrades(runID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	resp["trades"] = trades
	return c.JSON(resp)
}

func loadPublicTrades(runID string) ([]models.RunTrade, error) {
	rows, err := database.DB.Query(`
		SELECT id, run_id, symbol, side, quantity, fill_price, slippage_bps,
		       sim_time_filled, total_value, realized_pnl, COALESCE(reasoning, '')
		  FROM run_trades
		 WHERE run_id = ?1
		 ORDER BY sim_time_filled ASC, id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trades := []models.RunTrade{}
	for rows.Next() {
		var t models.RunTrade
		var filledAt string
		if err := rows.Scan(
			&t.ID, &t.RunID, &t.Symbol, &t.Side, &t.Quantity, &t.FillPrice,
			&t.SlippageBps, &filledAt, &t.TotalValue, &t.RealizedPnL, &t.Reasoning,
		); err != nil {
			return nil, err
		}
		t.SimTimeFilled, _ = time.Parse(time.RFC3339, filledAt)
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

func nullFloat(n sql.NullFloat64) interface{} {
	if !n.Valid {
		return nil
	}
	return n.Float64
}
