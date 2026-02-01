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
	// Use separate subqueries to avoid JOIN multiplication
	query := `
		WITH bot_positions AS (
			SELECT
				bot_id,
				COALESCE(SUM(quantity * avg_cost), 0) as positions_cost_basis
			FROM positions
			GROUP BY bot_id
		),
		bot_trades AS (
			SELECT
				bot_id,
				COUNT(*) as trade_count
			FROM trades
			GROUP BY bot_id
		)
		SELECT
			b.id,
			b.name,
			b.cash_balance,
			COALESCE(bp.positions_cost_basis, 0) as positions_value,
			COALESCE(bt.trade_count, 0) as trade_count
		FROM bots b
		LEFT JOIN bot_positions bp ON b.id = bp.bot_id
		LEFT JOIN bot_trades bt ON b.id = bt.bot_id
		WHERE b.is_active = true
		ORDER BY (b.cash_balance + COALESCE(bp.positions_cost_basis, 0)) DESC
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
		var cashBalance float64
		var positionsValue float64

		err := rows.Scan(
			&botID,
			&entry.BotName,
			&cashBalance,
			&positionsValue,
			&entry.TradeCount,
		)
		if err != nil {
			continue
		}

		// Calculate totals from cash + positions cost basis
		entry.TotalValue = cashBalance + positionsValue
		entry.PnL = entry.TotalValue - 100000.0
		entry.PnLPercent = (entry.PnL / 100000.0) * 100.0
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
