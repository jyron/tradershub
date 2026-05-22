package middleware

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RequireAPIKeyV1 is the auth middleware for the new Benchmark API at /v1/*.
// It validates X-API-Key against the bots.api_key column the same way the
// legacy RequireAPIKey does, but:
//
//   - Does NOT require bot.Claimed = 1. Benchmark-API users are pure API
//     consumers and may never go through the claim-via-UI flow.
//   - Does NOT enforce the per-bot daily trade cap. Benchmark scenarios are
//     deterministic replays; the cap exists for the live arena and has no
//     analog here.
//
// On success the bot is stashed at c.Locals("bot", bot) — identical contract
// to the legacy middleware so handlers can use the same GetBot/GetBotID
// helpers.
func RequireAPIKeyV1(c *fiber.Ctx) error {
	apiKey := c.Get("X-API-Key")
	if apiKey == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{"code": "missing_api_key", "message": "X-API-Key header required"},
		})
	}

	var bot models.Bot
	var botIDStr, createdAt string
	var isActive, claimed, isTest int
	var tier, disabledReason sql.NullString
	err := database.DB.QueryRow(
		`SELECT id, name, api_key, description, creator_email, cash_balance, created_at,
		        is_active, claimed, is_test, COALESCE(tier,''), disabled_reason
		   FROM bots
		  WHERE api_key = ?1 AND is_active = 1`,
		apiKey,
	).Scan(
		&botIDStr, &bot.Name, &bot.APIKey, &bot.Description,
		&bot.CreatorEmail, &bot.CashBalance, &createdAt, &isActive, &claimed, &isTest,
		&tier, &disabledReason,
	)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid_api_key", "message": "Invalid API key"},
		})
	}

	bot.ID, err = uuid.Parse(botIDStr)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{"code": "internal_error", "message": "Invalid bot ID"},
		})
	}

	bot.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		bot.CreatedAt = time.Now()
	}

	bot.IsActive = isActive != 0
	bot.Claimed = claimed != 0
	bot.IsTest = isTest != 0
	bot.Tier = tier.String

	// Honor disabled-for-cause short-circuit even on benchmark API — if a
	// key is disabled for abuse it should be locked out everywhere.
	if disabledReason.Valid && disabledReason.String != "" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": fiber.Map{
				"code":            "bot_disabled",
				"message":         "Bot is disabled",
				"disabled_reason": disabledReason.String,
			},
		})
	}

	c.Locals("bot", bot)
	return c.Next()
}
