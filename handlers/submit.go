package handlers

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const backfillDays = 30

// normalizeSubmitProvider accepts a few common spellings and clamps to the
// set the runner knows how to dispatch. Three native dispatch targets:
//   - openai_compat: any /v1/chat/completions endpoint (OpenAI, xAI, OpenRouter,
//     Together, Groq, DeepSeek, local llama.cpp). Most flexible.
//   - anthropic: native Anthropic Messages API (Claude).
//   - google:    native google-genai SDK (Gemini).
func normalizeSubmitProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "openai", "openai_compat", "openai-compatible", "openrouter", "xai", "grok", "together", "groq", "deepseek":
		return "openai_compat"
	case "anthropic", "claude":
		return "anthropic"
	case "google", "gemini":
		return "google"
	default:
		return ""
	}
}

// modelProviderTagFromSubmit derives the short tag used by the existing
// model_provider column so the UI's provider chip renders something sensible.
func modelProviderTagFromSubmit(provider, modelID string) string {
	switch provider {
	case "anthropic":
		return "claude"
	case "google":
		return "gemini"
	}
	low := strings.ToLower(modelID)
	switch {
	case strings.Contains(low, "claude"):
		return "claude"
	case strings.Contains(low, "gemini"):
		return "gemini"
	case strings.Contains(low, "grok"):
		return "grok"
	case strings.Contains(low, "llama"):
		return "meta"
	case strings.HasPrefix(low, "gpt") || strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4"):
		return "gpt"
	}
	return ""
}

// SubmitBot is the hosted-bot submission endpoint. The submitter pastes
// their LLM API key; we encrypt it with services.Keyvault and queue a
// 30-day backfill replay. They get back the bot's BotTrade API key and a
// job id they can poll.
func SubmitBot(c *fiber.Ctx) error {
	if services.Vault() == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Submissions are temporarily disabled (server master key not configured)",
		})
	}

	var req models.SubmitBotRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	req.Name = strings.TrimSpace(req.Name)
	req.ModelID = strings.TrimSpace(req.ModelID)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.ContactEmail = strings.TrimSpace(req.ContactEmail)

	provider := normalizeSubmitProvider(req.Provider)
	if provider == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Unsupported provider. Use one of: openai_compat, anthropic, google.",
		})
	}
	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Bot name is required"})
	}
	if req.ModelID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "model_id is required"})
	}
	if req.APIKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "api_key is required"})
	}

	encKey, nonce, version, err := services.Vault().Encrypt([]byte(req.APIKey))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to encrypt API key",
		})
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate API key",
		})
	}

	botID := uuid.New()
	providerTag := modelProviderTagFromSubmit(provider, req.ModelID)

	// Submission *is* claim: the submitter proved possession of an LLM key,
	// they don't need to click a separate claim banner.
	_, err = database.DB.Exec(
		`INSERT INTO bots
		   (id, name, api_key, description, creator_email, is_test, model_provider, is_official, claimed, tier)
		 VALUES (?1, ?2, ?3, ?4, ?5, 0, ?6, 0, 1, 'challenger')`,
		botID.String(), req.Name, apiKey, req.Description, req.ContactEmail,
		nullIfEmpty(providerTag),
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to register bot",
			"details": err.Error(),
		})
	}

	_, err = database.DB.Exec(
		`INSERT INTO bot_credentials
		   (bot_id, provider, base_url, model_id, encrypted_key, nonce, key_version)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		botID.String(), provider, nullIfEmpty(req.BaseURL), req.ModelID,
		encKey, nonce, version,
	)
	if err != nil {
		// Rollback: drop the bot row so the operator can retry cleanly.
		database.DB.Exec(`DELETE FROM bots WHERE id = ?`, botID.String())
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to store credentials",
			"details": err.Error(),
		})
	}

	jobID := uuid.NewString()
	_, err = database.DB.Exec(
		`INSERT INTO backfill_jobs (id, bot_id, status, days_requested)
		 VALUES (?, ?, 'queued', ?)`,
		jobID, botID.String(), backfillDays,
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to queue backfill",
			"details": err.Error(),
		})
	}

	protocol := "http"
	if c.Protocol() == "https" {
		protocol = "https"
	}
	publicURL := fmt.Sprintf("%s://%s/bots.html?id=%s", protocol, c.Hostname(), botID.String())

	return c.Status(fiber.StatusCreated).JSON(models.SubmitBotResponse{
		BotID:           botID,
		APIKey:          apiKey,
		BackfillJobID:   jobID,
		PublicURL:       publicURL,
		StartingBalance: 100000.00,
	})
}
