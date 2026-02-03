package middleware

import (
	"bottrade/database"
	"bottrade/models"
	"time"

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
	var botIDStr, createdAt string
	var isActive, claimed, isTest int
	err := database.DB.QueryRow(
		`SELECT id, name, api_key, description, creator_email, cash_balance, created_at, is_active, claimed, is_test
		 FROM bots
		 WHERE api_key = ?1 AND is_active = 1`,
		apiKey,
	).Scan(
		&botIDStr, &bot.Name, &bot.APIKey, &bot.Description,
		&bot.CreatorEmail, &bot.CashBalance, &createdAt, &isActive, &claimed, &isTest,
	)

	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid API key",
		})
	}

	// Parse the ID string back to UUID
	bot.ID, err = uuid.Parse(botIDStr)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid bot ID format",
		})
	}

	// Parse created_at timestamp
	bot.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		bot.CreatedAt = time.Now()
	}

	// Convert INTEGER to bool
	bot.IsActive = isActive != 0
	bot.Claimed = claimed != 0
	bot.IsTest = isTest != 0

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
