package apiv1

import (
	"bottrade/database"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"
)

// issueKeyRequest is the optional JSON body for POST /v1/keys. Both fields
// are optional — an empty POST is valid and produces an anonymous key.
type issueKeyRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type issueKeyResponse struct {
	APIKey string `json:"api_key"`
	BotID  string `json:"bot_id"`
	Name   string `json:"name"`
}

// mountKeyIssuer registers POST /v1/keys directly on Fiber, OUTSIDE huma, so
// it is the one /v1/* route that doesn't require X-API-Key. This is the
// frictionless self-serve entrypoint documented in agent.md — a user can
// curl this and immediately start hitting the rest of /v1/*.
//
// The endpoint creates a bots row tagged tier='challenger' with no LLM
// credentials and no backfill job, so it never enters the hosted-bot
// runner queue or the official leaderboard. It exists solely to authorise
// the Benchmark API for the caller's own self-hosted agent.
func (h *handlers) mountKeyIssuer(app *fiber.App) {
	keysLimiter := limiter.New(limiter.Config{
		Max:        10,
		Expiration: time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many key requests from this IP. Try again in an hour.",
			})
		},
	})

	app.Post("/v1/keys", keysLimiter, issueKey)
}

func issueKey(c *fiber.Ctx) error {
	var req issueKeyRequest
	// An empty body is fine. Only flag JSON that's actually malformed.
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid JSON body",
			})
		}
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)

	if len(req.Name) > 60 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "name must be 60 characters or fewer",
		})
	}
	if len(req.Email) > 254 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "email is too long",
		})
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate API key",
		})
	}

	botID := uuid.New()
	name := req.Name
	if name == "" {
		name = "agent-" + botID.String()[:8]
	}

	// claimed=1 because possession of the key is itself the claim — there's
	// no separate ownership flow for self-hosted agents. tier='challenger'
	// keeps the bot off the official leaderboard view; without credentials
	// or a backfill_jobs row it also never enters the hosted-runner queue.
	emailVal := interface{}(nil)
	if req.Email != "" {
		emailVal = req.Email
	}
	_, err = database.DB.Exec(
		`INSERT INTO bots
		   (id, name, api_key, description, creator_email, is_test, is_official, claimed, tier)
		 VALUES (?1, ?2, ?3, '', ?4, 0, 0, 1, 'challenger')`,
		botID.String(), name, apiKey, emailVal,
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to register bot",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(issueKeyResponse{
		APIKey: apiKey,
		BotID:  botID.String(),
		Name:   name,
	})
}

// generateAPIKey returns 64 hex chars of CSPRNG output. Local to this
// package to avoid coupling to handlers/bots.go — the implementation is
// three lines and copy-on-purpose so the v1 surface has no reverse
// dependency on the legacy fiber handlers.
func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
