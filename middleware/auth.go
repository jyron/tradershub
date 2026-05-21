package middleware

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"database/sql"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// tradePathSuffixes is the set of route suffixes for which we apply the
// per-bot daily trade cap. Other authenticated reads (portfolio fetches,
// enrollment, etc.) shouldn't count against the cap.
var tradePathSuffixes = []string{"/trade/stock", "/trade/option"}

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
			"error": "Invalid API key",
		})
	}

	bot.ID, err = uuid.Parse(botIDStr)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Invalid bot ID format",
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

	if !bot.Claimed {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Bot must be claimed before trading. Visit your claim URL to activate your bot.",
		})
	}

	// Auto-disable short-circuit: applies to every authenticated route so
	// a disabled bot can't bypass via /api/portfolio either.
	if disabledReason.Valid && disabledReason.String != "" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":           "Bot is disabled",
			"disabled_reason": disabledReason.String,
		})
	}

	// Daily trade cap: only enforced on trade endpoints, and exempted for
	// official bots. Counter increment is best-effort and non-blocking;
	// the cap *check* must succeed.
	path := c.Path()
	isTradeCall := false
	for _, suffix := range tradePathSuffixes {
		if strings.HasSuffix(path, suffix) {
			isTradeCall = true
			break
		}
	}
	if isTradeCall {
		capped, _ := services.IsCapped(botIDStr, "trade_count", bot.Tier)
		if capped {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "daily trade cap reached",
			})
		}
		_ = services.RecordTrade(botIDStr)
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
