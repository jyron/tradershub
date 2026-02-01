package handlers

import (
	"bottrade/database"
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type LeaderboardEntry struct {
	Rank        int     `json:"rank"`
	BotID       string  `json:"bot_id"`
	BotName     string  `json:"bot_name"`
	TotalValue  float64 `json:"total_value"`
	PnL         float64 `json:"pnl"`
	PnLPercent  float64 `json:"pnl_percent"`
	TradeCount  int     `json:"trade_count"`
}

type LeaderboardResponse struct {
	Period   string             `json:"period"`
	Rankings []LeaderboardEntry `json:"rankings"`
}

func GetLeaderboard(c *fiber.Ctx) error {
	period := c.Query("period", "all")
	limitStr := c.Query("limit", "50")

	var limit int
	if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	// Calculate portfolio values for all bots
	query := `
		WITH bot_portfolios AS (
			SELECT
				b.id,
				b.name,
				b.cash_balance,
				COALESCE(SUM(p.quantity * p.avg_cost), 0) as positions_value,
				COUNT(t.id) as trade_count
			FROM bots b
			LEFT JOIN positions p ON b.id = p.bot_id
			LEFT JOIN trades t ON b.id = t.bot_id
			WHERE b.is_active = true
			GROUP BY b.id, b.name, b.cash_balance
		)
		SELECT
			id,
			name,
			cash_balance + positions_value as total_value,
			(cash_balance + positions_value - 100000) as pnl,
			((cash_balance + positions_value - 100000) / 100000.0 * 100) as pnl_percent,
			trade_count
		FROM bot_portfolios
		ORDER BY total_value DESC
		LIMIT $1
	`

	rows, err := database.DB.Query(context.Background(), query, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch leaderboard",
		})
	}
	defer rows.Close()

	rankings := []LeaderboardEntry{}
	rank := 1

	for rows.Next() {
		var entry LeaderboardEntry
		var botID string
		err := rows.Scan(
			&botID,
			&entry.BotName,
			&entry.TotalValue,
			&entry.PnL,
			&entry.PnLPercent,
			&entry.TradeCount,
		)
		if err != nil {
			continue
		}

		entry.Rank = rank
		entry.BotID = botID
		rankings = append(rankings, entry)
		rank++
	}

	return c.JSON(LeaderboardResponse{
		Period:   period,
		Rankings: rankings,
	})
}
