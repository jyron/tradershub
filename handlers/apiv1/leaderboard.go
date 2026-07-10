package apiv1

import (
	"bottrade/database"
	"database/sql"

	"github.com/gofiber/fiber/v2"
)

// mountLeaderboardPublic registers GET /api/v1/leaderboard and
// GET /api/v1/leaderboard/scenarios outside huma so they can be hit without
// an X-API-Key. Both endpoints return data that is already public by
// design (the published run_leaderboard table).
func (h *handlers) mountLeaderboardPublic(app *fiber.App) {
	app.Get("/api/v1/leaderboard/scenarios", listLeaderboardScenarios)
	app.Get("/api/v1/leaderboard", getLeaderboard)
}

type leaderboardEntry struct {
	Rank        int     `json:"rank"`
	RunID       string  `json:"run_id"`
	APIKeyID    string  `json:"api_key_id"`
	BotName     string  `json:"bot_name"`
	ReturnPct   float64 `json:"return_pct"`
	Sharpe      float64 `json:"sharpe,omitempty"`
	Sortino     float64 `json:"sortino,omitempty"`
	MaxDrawdown float64 `json:"max_drawdown,omitempty"`
	FinalEquity float64 `json:"final_equity"`
	TradeCount  int     `json:"trade_count"`
	Liquidated  bool    `json:"liquidated"`
	PublishedAt string  `json:"published_at"`
}

type leaderboardScenarioInfo struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	RunCount       int    `json:"run_count"`
	PublishedCount int    `json:"published_count"`
}

func listLeaderboardScenarios(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT s.id, s.slug, s.name,
		       (SELECT COUNT(*) FROM runs r WHERE r.scenario_id = s.id) AS run_count,
		       (SELECT COUNT(*) FROM run_leaderboard l WHERE l.scenario_id = s.id) AS pub_count
		  FROM scenarios s
		 WHERE s.status IN ('ready','archived')
		 ORDER BY pub_count DESC, s.created_at DESC
	`)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer rows.Close()

	out := []leaderboardScenarioInfo{}
	for rows.Next() {
		var s leaderboardScenarioInfo
		if err := rows.Scan(&s.ID, &s.Slug, &s.Name, &s.RunCount, &s.PublishedCount); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		out = append(out, s)
	}
	return c.JSON(fiber.Map{"scenarios": out})
}

func getLeaderboard(c *fiber.Ctx) error {
	scenarioParam := c.Query("scenario")
	if scenarioParam == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "scenario query param required"})
	}

	var scenarioID, scenarioSlug string
	if err := database.DB.QueryRow(
		`SELECT id, slug FROM scenarios WHERE id = ?1 OR slug = ?1`,
		scenarioParam,
	).Scan(&scenarioID, &scenarioSlug); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no scenario " + scenarioParam})
	}

	sortBy := c.Query("sort_by", "return")
	orderCol := "rr.return_pct DESC"
	switch sortBy {
	case "sharpe":
		orderCol = "rr.sharpe DESC"
	case "sortino":
		orderCol = "rr.sortino DESC"
	case "max_drawdown":
		orderCol = "rr.max_drawdown ASC"
	}

	limit := c.QueryInt("limit", 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	q := `
		SELECT l.run_id, l.api_key_id,
		       rr.return_pct, rr.sharpe, rr.sortino, rr.max_drawdown,
		       rr.final_equity, rr.trade_count, rr.liquidated,
		       l.published_at,
		       COALESCE(NULLIF(l.bot_name, ''), k.name),
		       a.handle,
		       a.plan
		  FROM run_leaderboard l
		  JOIN run_results    rr ON rr.run_id = l.run_id
		  JOIN api_keys       k  ON k.id     = l.api_key_id
		  JOIN accounts       a  ON a.id     = COALESCE(k.account_id, k.id)
		 WHERE l.scenario_id = ?1
		 ORDER BY ` + orderCol + `
		 LIMIT ?2
	`
	rows, err := database.DB.Query(q, scenarioID, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer rows.Close()

	entries := []leaderboardEntry{}
	rank := 0
	for rows.Next() {
		rank++
		var e leaderboardEntry
		var sharpe, sortino, maxDD sql.NullFloat64
		var liquidated int
		var rawBotName string
		var handle, plan sql.NullString
		if err := rows.Scan(
			&e.RunID, &e.APIKeyID,
			&e.ReturnPct, &sharpe, &sortino, &maxDD,
			&e.FinalEquity, &e.TradeCount, &liquidated,
			&e.PublishedAt,
			&rawBotName,
			&handle,
			&plan,
		); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		e.Rank = rank
		e.Sharpe = sharpe.Float64
		e.Sortino = sortino.Float64
		e.MaxDrawdown = maxDD.Float64
		e.Liquidated = liquidated != 0

		hasProHandle := (plan.String == "pro" || plan.String == "max") && handle.Valid && handle.String != ""
		if hasProHandle {
			e.BotName = handle.String + " — " + rawBotName
		} else {
			e.BotName = rawBotName
		}

		entries = append(entries, e)
	}
	return c.JSON(fiber.Map{
		"scenario": scenarioSlug,
		"sort_by":  sortBy,
		"entries":  entries,
	})
}
