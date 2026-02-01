package handlers

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

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

	var botID uuid.UUID
	err = database.DB.QueryRow(
		context.Background(),
		`INSERT INTO bots (name, api_key, description, creator_email)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		req.Name, apiKey, req.Description, req.CreatorEmail,
	).Scan(&botID)

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
	err = database.DB.QueryRow(
		context.Background(),
		`SELECT id, name, description, creator_email, cash_balance, created_at, is_active, claimed
		 FROM bots WHERE id = $1`,
		botID,
	).Scan(&bot.ID, &bot.Name, &bot.Description, &bot.CreatorEmail, &bot.CashBalance, &bot.CreatedAt, &bot.IsActive, &bot.Claimed)

	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Bot not found",
		})
	}

	// Get portfolio
	portfolioService := services.NewPortfolioService()
	portfolio, err := portfolioService.GetPortfolio(botID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch portfolio",
		})
	}

	// Get recent trades
	rows, err := database.DB.Query(
		context.Background(),
		`SELECT id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
		 FROM trades WHERE bot_id = $1 ORDER BY executed_at DESC LIMIT 20`,
		botID,
	)
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
		context.Background(),
		"SELECT COUNT(*) FROM trades WHERE bot_id = $1",
		botID,
	).Scan(&tradeCount)

	return c.JSON(fiber.Map{
		"id":            bot.ID,
		"name":          bot.Name,
		"description":   bot.Description,
		"creator_email": bot.CreatorEmail,
		"created_at":    bot.CreatedAt,
		"claimed":       bot.Claimed,
		"portfolio":     portfolio,
		"recent_trades": trades,
		"trade_count":   tradeCount,
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
		context.Background(),
		`UPDATE bots SET claimed = true WHERE id = $1 AND claimed = false`,
		botID,
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to claim bot",
		})
	}

	rowsAffected := result.RowsAffected()
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
