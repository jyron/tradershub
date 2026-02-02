package handlers

import (
	"bottrade/database"
	"bottrade/services"
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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

	ctx := context.Background()

	// Get all active bots with their basic info and trade counts
	query := `
		SELECT
			b.id,
			b.name,
			b.cash_balance,
			COALESCE(COUNT(t.id), 0) as trade_count
		FROM bots b
		LEFT JOIN trades t ON b.id = t.bot_id
		WHERE b.is_active = true
		GROUP BY b.id, b.name, b.cash_balance
	`

	rows, err := database.DB.Query(ctx, query)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch leaderboard",
		})
	}
	defer rows.Close()

	portfolioService := services.NewPortfolioService()
	var entries []LeaderboardEntry

	for rows.Next() {
		var botID uuid.UUID
		var botName string
		var cashBalance float64
		var tradeCount int

		err := rows.Scan(&botID, &botName, &cashBalance, &tradeCount)
		if err != nil {
			continue
		}

		// Calculate actual portfolio value using market prices
		portfolio, err := portfolioService.GetPortfolio(botID)
		if err != nil {
			// If we can't calculate portfolio value, skip this bot
			continue
		}

		entry := LeaderboardEntry{
			BotID:      botID.String(),
			BotName:    botName,
			TotalValue: portfolio.TotalValue,
			PnL:        portfolio.TotalPnL,
			PnLPercent: portfolio.TotalPnLPct,
			TradeCount: tradeCount,
		}

		entries = append(entries, entry)
	}

	// Sort by total value descending
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].TotalValue > entries[i].TotalValue {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Apply limit and assign ranks
	if len(entries) > limit {
		entries = entries[:limit]
	}

	for i := range entries {
		entries[i].Rank = i + 1
	}

	return c.JSON(LeaderboardResponse{
		Period:   period,
		Rankings: entries,
	})
}
