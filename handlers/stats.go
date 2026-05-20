package handlers

import (
	"bottrade/database"
	"time"

	"github.com/gofiber/fiber/v2"
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
	if cached, ok := statsCache.get("global"); ok {
		return c.JSON(cached)
	}

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

	// Count active bots (official only — junk user bots stay hidden from
	// the public stats page).
	var activeBotsCount int
	err = database.DB.QueryRow(
		`SELECT COUNT(*) FROM bots WHERE is_active = 1 AND COALESCE(is_official, 0) = 1`,
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

	// Biggest gainer / loser from the latest snapshot per bot. One SQL query
	// instead of an N+1 GetPortfolio() loop with per-position Finnhub calls.
	const startingBalance = 100000.0
	botsRows, _ := database.DB.Query(`
		SELECT b.id, b.name,
		       COALESCE((SELECT total_value FROM portfolio_snapshots
		                 WHERE bot_id = b.id AND season_id IS NULL
		                 ORDER BY snapshot_at DESC LIMIT 1),
		                b.cash_balance,
		                ?1) AS total_value
		FROM bots b
		WHERE b.is_active = 1 AND COALESCE(b.is_official, 0) = 1
	`, startingBalance)
	defer botsRows.Close()

	var biggestGainer *BotGainerInfo
	var biggestLoser *BotGainerInfo
	maxGain := -1000000.0
	maxLoss := 1000000.0

	for botsRows.Next() {
		var botID, botName string
		var totalValue float64
		if err := botsRows.Scan(&botID, &botName, &totalValue); err != nil {
			continue
		}
		pnlPercent := ((totalValue - startingBalance) / startingBalance) * 100

		if pnlPercent > maxGain {
			maxGain = pnlPercent
			biggestGainer = &BotGainerInfo{
				BotID:      botID,
				BotName:    botName,
				PnLPercent: pnlPercent,
			}
		}

		if pnlPercent < maxLoss {
			maxLoss = pnlPercent
			biggestLoser = &BotGainerInfo{
				BotID:      botID,
				BotName:    botName,
				PnLPercent: pnlPercent,
			}
		}
	}

	resp := StatsResponse{
		RecentTradesCount: recentTradesCount,
		ActiveBotsCount:   activeBotsCount,
		PopularSymbols:    popularSymbols,
		BiggestGainer:     biggestGainer,
		BiggestLoser:      biggestLoser,
	}
	statsCache.put("global", resp)
	return c.JSON(resp)
}
