package handlers

import (
	"bottrade/database"
	"bottrade/services"
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type LeaderboardEntry struct {
	Rank          int     `json:"rank"`
	BotID         string  `json:"bot_id"`
	BotName       string  `json:"bot_name"`
	TotalValue    float64 `json:"total_value"`
	PnL           float64 `json:"pnl"`
	PnLPercent    float64 `json:"pnl_percent"`
	TradeCount    int     `json:"trade_count"`
	RankChange    int     `json:"rank_change"`     // Positive = moved up, negative = moved down
	PreviousRank  int     `json:"previous_rank"`   // Rank from yesterday
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

	// Get previous day's rankings for comparison
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	previousRanks := make(map[string]int)

	prevRows, err := database.DB.Query(ctx, `
		SELECT bot_id, rank
		FROM ranking_snapshots
		WHERE snapshot_date = $1
	`, yesterday)

	if err == nil {
		defer prevRows.Close()
		for prevRows.Next() {
			var botID string
			var rank int
			if err := prevRows.Scan(&botID, &rank); err == nil {
				previousRanks[botID] = rank
			}
		}
	}

	// Assign ranks and calculate rank changes
	for i := range entries {
		entries[i].Rank = i + 1
		if prevRank, exists := previousRanks[entries[i].BotID]; exists {
			entries[i].PreviousRank = prevRank
			entries[i].RankChange = prevRank - entries[i].Rank // Positive = moved up
		}
	}

	// Save today's rankings for tomorrow's comparison
	today := time.Now().Format("2006-01-02")
	for _, entry := range entries {
		botUUID, _ := uuid.Parse(entry.BotID)
		database.DB.Exec(ctx, `
			INSERT INTO ranking_snapshots (bot_id, rank, total_value, snapshot_date)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (bot_id, snapshot_date)
			DO UPDATE SET rank = $2, total_value = $3
		`, botUUID, entry.Rank, entry.TotalValue, today)
	}

	return c.JSON(LeaderboardResponse{
		Period:   period,
		Rankings: entries,
	})
}
