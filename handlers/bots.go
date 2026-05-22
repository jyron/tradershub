package handlers

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// sqlNullString is a local alias to keep the call sites short.
type sqlNullString = sql.NullString

// normalizeProvider lowercases and canonicalizes the model_provider hint.
// We accept a small set of values; anything else is dropped to "" so the UI
// doesn't render an unknown chip.
func normalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "claude", "anthropic", "anth":
		return "claude"
	case "gpt", "openai", "oai":
		return "gpt"
	case "gemini", "google", "goog":
		return "gemini"
	case "grok", "xai":
		return "grok"
	case "meta", "llama":
		return "meta"
	default:
		return ""
	}
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

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

	// Normalize model_provider to a short lowercase token. Empty stays empty
	// (legacy / user-registered bots without a provider hint).
	provider := normalizeProvider(req.ModelProvider)

	// Baselines and officials are seeded with tier='official' so they appear
	// on the main board immediately, exempt from the per-day trade cap, and
	// don't have to go through the challenger → verified promotion path.
	tier := "challenger"
	if req.IsOfficial || req.IsBaseline {
		tier = "official"
	}
	_, err = database.DB.Exec(
		`INSERT INTO bots (id, name, api_key, description, creator_email, is_test, model_provider, is_official, is_baseline, tier)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
		botID.String(), req.Name, apiKey, req.Description, req.CreatorEmail,
		req.IsTest, nullIfEmpty(provider), boolToInt(req.IsOfficial), boolToInt(req.IsBaseline), tier,
	)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to register bot",
		})
	}

	// A freshly-registered bot should be visible immediately rather than
	// hidden behind a 30s cache TTL — also lets seed_baselines.py re-run
	// idempotently without racing the cache.
	leaderboardCache.clear()

	// Claim flow lives on the bot profile page (?claim=1 surfaces the claim banner).
	protocol := "http"
	if c.Protocol() == "https" {
		protocol = "https"
	}
	claimURL := fmt.Sprintf("%s://%s/bots.html?id=%s&claim=1", protocol, c.Hostname(), botID.String())

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
	var isActive, claimed, isTest, isOfficial int
	var modelProvider, description, creatorEmail sqlNullString
	err = database.DB.QueryRow(
		`SELECT id, name, description, creator_email, cash_balance, created_at,
		        is_active, claimed, is_test, model_provider, is_official
		 FROM bots WHERE id = ?1`,
		botID.String(),
	).Scan(&dbBotID, &bot.Name, &description, &creatorEmail, &bot.CashBalance, &createdAt,
		&isActive, &claimed, &isTest, &modelProvider, &isOfficial)
	bot.Description = description.String
	bot.CreatorEmail = creatorEmail.String
	bot.ModelProvider = modelProvider.String
	bot.IsOfficial = isOfficial != 0

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
		  FROM trades WHERE bot_id = ?1 AND season_id IS NULL`
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
		// reasoning is nullable; executed_at is SQLite TEXT and modernc.org/sqlite
		// hands it back as a string, so we scan it as one and parse it ourselves
		// rather than letting database/sql reject the row silently.
		var reasoning sqlNullString
		var executedAtStr string
		err := rows.Scan(
			&trade.ID, &trade.Symbol, &trade.TradeType, &trade.Side,
			&trade.Quantity, &trade.Price, &trade.TotalValue, &reasoning, &executedAtStr,
		)
		if err != nil {
			continue
		}
		trade.Reasoning = reasoning.String
		if t, perr := time.Parse("2006-01-02 15:04:05", executedAtStr); perr == nil {
			trade.ExecutedAt = t
		} else if t, perr := time.Parse(time.RFC3339, executedAtStr); perr == nil {
			trade.ExecutedAt = t
		}
		trades = append(trades, trade)
	}

	// Count total trades
	var tradeCount int
	database.DB.QueryRow(
		"SELECT COUNT(*) FROM trades WHERE bot_id = ?1 AND season_id IS NULL",
		botID.String(),
	).Scan(&tradeCount)

	// Get portfolio snapshots for historical chart (daily mark-to-market from generate_snapshots.py)
	snapshotRows, errSnap := database.DB.Query(
		`SELECT snapshot_at, total_value FROM portfolio_snapshots
		 WHERE bot_id = ?1 AND season_id IS NULL ORDER BY snapshot_at ASC`,
		botID.String(),
	)
	portfolioSnapshots := []fiber.Map{}
	if errSnap == nil && snapshotRows != nil {
		defer snapshotRows.Close()
		for snapshotRows.Next() {
			// snapshot_at is SQLite TEXT; see note in the trade loop above for
			// why we don't scan straight into time.Time.
			var snapshotAtStr string
			var totalValue float64
			if err := snapshotRows.Scan(&snapshotAtStr, &totalValue); err != nil {
				continue
			}
			var snapshotAt time.Time
			if t, perr := time.Parse("2006-01-02 15:04:05", snapshotAtStr); perr == nil {
				snapshotAt = t
			} else if t, perr := time.Parse(time.RFC3339, snapshotAtStr); perr == nil {
				snapshotAt = t
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
		"model_provider":      bot.ModelProvider,
		"is_official":         bot.IsOfficial,
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
