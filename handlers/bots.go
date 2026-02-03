package handlers

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func RegisterBot(c *fiber.Ctx) error {
	var req models.RegisterBotRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Bot name is required",
		})
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate API key",
		})
	}

	// Generate UUID for the bot
	botID := uuid.New()

	_, err = database.DB.Exec(
		`INSERT INTO bots (id, name, api_key, description, creator_email, is_test)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
		botID.String(), req.Name, apiKey, req.Description, req.CreatorEmail, req.IsTest,
	)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to register bot",
		})
	}

	// Construct claim URL (claim.html?id= so static server can serve it)
	protocol := "http"
	if c.Protocol() == "https" {
		protocol = "https"
	}
	claimURL := fmt.Sprintf("%s://%s/claim.html?id=%s", protocol, c.Hostname(), botID.String())

	return c.Status(fiber.StatusCreated).JSON(models.RegisterBotResponse{
		BotID:           botID,
		APIKey:          apiKey,
		ClaimURL:        claimURL,
		StartingBalance: 100000.00,
	})
}

func GetBotDetails(c *fiber.Ctx) error {
	botIDStr := c.Params("bot_id")
	botID, err := uuid.Parse(botIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid bot ID",
		})
	}

	// Get bot info
	var bot models.Bot
	var dbBotID, createdAt string
	var isActive, claimed, isTest int
	err = database.DB.QueryRow(
		`SELECT id, name, description, creator_email, cash_balance, created_at, is_active, claimed, is_test
		 FROM bots WHERE id = ?1`,
		botID.String(),
	).Scan(&dbBotID, &bot.Name, &bot.Description, &bot.CreatorEmail, &bot.CashBalance, &createdAt, &isActive, &claimed, &isTest)

	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Bot not found",
			"details": err.Error(),
		})
	}

	// Parse the ID string back to UUID
	bot.ID, err = uuid.Parse(dbBotID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid bot ID format",
		})
	}

	// Parse created_at timestamp (SQLite format: "2006-01-02 15:04:05")
	bot.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		// If parse fails, use current time as fallback
		bot.CreatedAt = time.Now()
	}

	// Convert INTEGER to bool
	bot.IsActive = isActive != 0
	bot.Claimed = claimed != 0
	bot.IsTest = isTest != 0

	// Get portfolio
	portfolioService := services.NewPortfolioService()
	portfolio, err := portfolioService.GetPortfolio(botID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch portfolio",
		})
	}

	// Get trades with optional limit and date range (for 1D/1W/1M/1Y filtering)
	limitStr := c.Query("limit", "50")
	var limit int
	if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if toStr != "" && len(toStr) == 10 {
		toStr = toStr + " 23:59:59"
	}

	query := `SELECT id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
		  FROM trades WHERE bot_id = ?1`
	args := []interface{}{botID.String()}
	argNum := 2
	if fromStr != "" {
		query += fmt.Sprintf(" AND executed_at >= ?%d", argNum)
		args = append(args, fromStr)
		argNum++
	}
	if toStr != "" {
		query += fmt.Sprintf(" AND executed_at <= ?%d", argNum)
		args = append(args, toStr)
		argNum++
	}
	query += fmt.Sprintf(" ORDER BY executed_at DESC LIMIT ?%d", argNum)
	args = append(args, limit)

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch trades",
		})
	}
	defer rows.Close()

	trades := []models.Trade{}
	for rows.Next() {
		var trade models.Trade
		err := rows.Scan(
			&trade.ID, &trade.Symbol, &trade.TradeType, &trade.Side,
			&trade.Quantity, &trade.Price, &trade.TotalValue, &trade.Reasoning, &trade.ExecutedAt,
		)
		if err != nil {
			continue
		}
		trades = append(trades, trade)
	}

	// Count total trades
	var tradeCount int
	database.DB.QueryRow(
		"SELECT COUNT(*) FROM trades WHERE bot_id = ?1",
		botID.String(),
	).Scan(&tradeCount)

	// Get portfolio snapshots for historical chart (daily mark-to-market from generate_snapshots.py)
	snapshotRows, errSnap := database.DB.Query(
		`SELECT snapshot_at, total_value FROM portfolio_snapshots WHERE bot_id = ?1 ORDER BY snapshot_at ASC`,
		botID.String(),
	)
	portfolioSnapshots := []fiber.Map{}
	if errSnap == nil && snapshotRows != nil {
		defer snapshotRows.Close()
		for snapshotRows.Next() {
			var snapshotAt time.Time
			var totalValue float64
			if err := snapshotRows.Scan(&snapshotAt, &totalValue); err != nil {
				continue
			}
			portfolioSnapshots = append(portfolioSnapshots, fiber.Map{
				"snapshot_at": snapshotAt,
				"total_value": totalValue,
			})
		}
	}

	return c.JSON(fiber.Map{
		"id":                  bot.ID,
		"name":                bot.Name,
		"description":         bot.Description,
		"creator_email":       bot.CreatorEmail,
		"created_at":          bot.CreatedAt,
		"claimed":             bot.Claimed,
		"portfolio":           portfolio,
		"recent_trades":       trades,
		"trade_count":         tradeCount,
		"portfolio_snapshots": portfolioSnapshots,
	})
}

func ClaimBot(c *fiber.Ctx) error {
	botIDStr := c.Params("bot_id")
	botID, err := uuid.Parse(botIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid bot ID",
		})
	}

	// Update bot to claimed
	result, err := database.DB.Exec(
		`UPDATE bots SET claimed = 1 WHERE id = ?1 AND claimed = 0`,
		botID.String(),
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to claim bot",
		})
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to check rows affected",
		})
	}
	if rowsAffected == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Bot not found or already claimed",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Bot claimed successfully! Your bot can now trade.",
	})
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
