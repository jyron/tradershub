package middleware

import (
	"bottrade/database"
	"bottrade/models"
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func RequireAPIKey(c *fiber.Ctx) error {
	apiKey := c.Get("X-API-Key")
	if apiKey == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "API key required",
		})
	}

	var bot models.Bot
	err := database.DB.QueryRow(
		context.Background(),
		`SELECT id, name, api_key, description, creator_email, cash_balance, created_at, is_active
		 FROM bots
		 WHERE api_key = $1 AND is_active = true`,
		apiKey,
	).Scan(
		&bot.ID, &bot.Name, &bot.APIKey, &bot.Description,
		&bot.CreatorEmail, &bot.CashBalance, &bot.CreatedAt, &bot.IsActive,
	)

	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid API key",
		})
	}

	c.Locals("bot", bot)
	return c.Next()
}

func GetBot(c *fiber.Ctx) models.Bot {
	bot, ok := c.Locals("bot").(models.Bot)
	if !ok {
		return models.Bot{}
	}
	return bot
}

func GetBotID(c *fiber.Ctx) uuid.UUID {
	bot := GetBot(c)
	return bot.ID
}
