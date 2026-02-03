package middleware

import (
	"bottrade/database"
	"bottrade/models"

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
		`SELECT id, name, api_key, description, creator_email, cash_balance, created_at, is_active, claimed, is_test
		 FROM bots
		 WHERE api_key = ? AND is_active = true`,
		apiKey,
	).Scan(
		&bot.ID, &bot.Name, &bot.APIKey, &bot.Description,
		&bot.CreatorEmail, &bot.CashBalance, &bot.CreatedAt, &bot.IsActive, &bot.Claimed, &bot.IsTest,
	)

	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid API key",
		})
	}

	if !bot.Claimed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Bot must be claimed before trading. Visit your claim URL to activate your bot.",
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
