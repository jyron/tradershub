package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ListScenarios returns every scenario in status='ready' or 'archived'.
// Drafts are admin-only so are hidden here.
func (h *Handlers) ListScenarios(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status, current_version, created_at
		  FROM scenarios
		 WHERE status IN ('ready', 'archived')
		 ORDER BY created_at DESC
	`)
	if err != nil {
		return jsonErrorf(c, fiber.StatusInternalServerError, "db_error", "%v", err)
	}
	defer rows.Close()

	out := []models.Scenario{}
	for rows.Next() {
		s := models.Scenario{}
		var startStr, endStr, createdStr, universeStr, slippageStr string
		var shortEnabled int
		if err := rows.Scan(
			&s.ID, &s.Slug, &s.Name, &s.Description, &s.BarResolution,
			&startStr, &endStr, &s.StartingCash, &s.LeverageCap, &shortEnabled,
			&universeStr, &slippageStr, &s.BenchmarkSymbol, &s.Status, &s.CurrentVersion, &createdStr,
		); err != nil {
			return jsonErrorf(c, fiber.StatusInternalServerError, "db_error", "%v", err)
		}
		s.StartTs, _ = time.Parse(time.RFC3339, startStr)
		s.EndTs, _ = time.Parse(time.RFC3339, endStr)
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		s.ShortEnabled = shortEnabled != 0
		s.Universe, _ = models.UnmarshalUniverse(universeStr)
		s.SlippageBps, _ = models.UnmarshalSlippage(slippageStr)
		out = append(out, s)
	}
	return c.JSON(fiber.Map{"scenarios": out})
}

// GetScenario returns full detail for a single scenario by id or slug.
func (h *Handlers) GetScenario(c *fiber.Ctx) error {
	idOrSlug := c.Params("id")
	row := database.DB.QueryRow(`
		SELECT id, slug, name, COALESCE(description,''), bar_resolution,
		       start_ts, end_ts, starting_cash, leverage_cap, short_enabled,
		       universe_json, slippage_json, benchmark_symbol, status, current_version, created_at
		  FROM scenarios
		 WHERE (id = ?1 OR slug = ?1)
		   AND status IN ('ready','archived')
	`, idOrSlug)

	s := models.Scenario{}
	var startStr, endStr, createdStr, universeStr, slippageStr string
	var shortEnabled int
	if err := row.Scan(
		&s.ID, &s.Slug, &s.Name, &s.Description, &s.BarResolution,
		&startStr, &endStr, &s.StartingCash, &s.LeverageCap, &shortEnabled,
		&universeStr, &slippageStr, &s.BenchmarkSymbol, &s.Status, &s.CurrentVersion, &createdStr,
	); err != nil {
		return jsonError(c, fiber.StatusNotFound, "scenario_not_found", "no such scenario")
	}
	s.StartTs, _ = time.Parse(time.RFC3339, startStr)
	s.EndTs, _ = time.Parse(time.RFC3339, endStr)
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	s.ShortEnabled = shortEnabled != 0
	s.Universe, _ = models.UnmarshalUniverse(universeStr)
	s.SlippageBps, _ = models.UnmarshalSlippage(slippageStr)
	return c.JSON(fiber.Map{"scenario": s})
}
