package handlers

import (
	"bottrade/database"
	"bottrade/services"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type StatsResponse struct {
	RecentTradesCount int            `json:"recent_trades_count"`
	ActiveBotsCount   int            `json:"active_bots_count"`
	PopularSymbols    []SymbolStats  `json:"popular_symbols"`
	BiggestGainer     *BotGainerInfo `json:"biggest_gainer"`
	BiggestLoser      *BotGainerInfo `json:"biggest_loser"`
}

type SymbolStats struct {
	Symbol    string `json:"symbol"`
	TradeCount int   `json:"trade_count"`
	BotCount   int   `json:"bot_count"`
}

type BotGainerInfo struct {
	BotID      string  `json:"bot_id"`
	BotName    string  `json:"bot_name"`
	PnLPercent float64 `json:"pnl_percent"`
}

func GetStats(c *fiber.Ctx) error {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Count recent trades (last hour)
	var recentTradesCount int
	err := database.DB.QueryRow(		`SELECT COUNT(*) FROM trades WHERE executed_at >= ?`,
		oneHourAgo,
	).Scan(&recentTradesCount)
	if err != nil {
		recentTradesCount = 0
	}

	// Count active bots
	var activeBotsCount int
	err = database.DB.QueryRow(		`SELECT COUNT(*) FROM bots WHERE is_active = true`,
	).Scan(&activeBotsCount)
	if err != nil {
		activeBotsCount = 0
	}

	// Get popular symbols (today's most traded)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	popularRows, err := database.DB.Query(`
		SELECT
			symbol,
			COUNT(*) as trade_count,
			COUNT(DISTINCT bot_id) as bot_count
		FROM trades
		WHERE executed_at >= ?
		GROUP BY symbol
		ORDER BY trade_count DESC
		LIMIT 5
	`, startOfDay)

	var popularSymbols []SymbolStats
	if err == nil {
		defer popularRows.Close()
		for popularRows.Next() {
			var s SymbolStats
			if err := popularRows.Scan(&s.Symbol, &s.TradeCount, &s.BotCount); err == nil {
				popularSymbols = append(popularSymbols, s)
			}
		}
	}

	// Get biggest gainer and loser (calculate from portfolio values)
	portfolioService := services.NewPortfolioService()

	var botsRows, _ = database.DB.Query(`
		SELECT id, name FROM bots WHERE is_active = true
	`)
	defer botsRows.Close()

	var biggestGainer *BotGainerInfo
	var biggestLoser *BotGainerInfo
	maxGain := -1000000.0
	maxLoss := 1000000.0

	for botsRows.Next() {
		var botID uuid.UUID
		var botName string
		if err := botsRows.Scan(&botID, &botName); err != nil {
			continue
		}

		portfolio, err := portfolioService.GetPortfolio(botID)
		if err != nil {
			continue
		}

		pnlPercent := portfolio.TotalPnLPct

		if pnlPercent > maxGain {
			maxGain = pnlPercent
			biggestGainer = &BotGainerInfo{
				BotID:      botID.String(),
				BotName:    botName,
				PnLPercent: pnlPercent,
			}
		}

		if pnlPercent < maxLoss {
			maxLoss = pnlPercent
			biggestLoser = &BotGainerInfo{
				BotID:      botID.String(),
				BotName:    botName,
				PnLPercent: pnlPercent,
			}
		}
	}

	return c.JSON(StatsResponse{
		RecentTradesCount: recentTradesCount,
		ActiveBotsCount:   activeBotsCount,
		PopularSymbols:    popularSymbols,
		BiggestGainer:     biggestGainer,
		BiggestLoser:      biggestLoser,
	})
}
