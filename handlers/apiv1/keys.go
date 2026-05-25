package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"
)

// issueKeyRequest is the optional JSON body for POST /api/v1/keys. All fields
// are optional — an empty POST is valid and produces an anonymous key.
type issueKeyRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type issueKeyResponse struct {
	APIKey    string `json:"api_key"`
	KeyID     string `json:"key_id"`
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Plan      string `json:"plan"`
}

// mountKeyIssuer registers POST /api/v1/keys directly on Fiber, OUTSIDE huma,
// so it is the one /api/v1/* route that doesn't require X-API-Key. This is the
// frictionless self-serve entrypoint. It creates a BotTrade account with one
// API key. The account owns usage and billing; the key is the reusable
// credential callers can use from scripts, REST clients, and MCP clients.
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

	app.Post("/api/v1/keys", keysLimiter, issueKey)
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

	resp, err := createAPIKey(req.Name, req.Email, "free")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate API key",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(resp)
}

func createAPIKey(requestedName, email, plan string) (issueKeyResponse, error) {
	accountID := uuid.New()
	return createAccountAPIKey(accountID, requestedName, email, plan)
}

func createAccountAPIKey(accountID uuid.UUID, requestedName, email, plan string) (issueKeyResponse, error) {
	apiKey, err := generateAPIKey()
	if err != nil {
		return issueKeyResponse{}, err
	}

	keyID := uuid.New()
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = "key-" + keyID.String()[:8]
	}
	email = strings.TrimSpace(email)
	plan = strings.TrimSpace(plan)
	if plan == "" {
		plan = "free"
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return issueKeyResponse{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.Exec(
		`INSERT INTO accounts
		   (id, name, email, billing_email, plan)
		 VALUES (?1, ?2, ?3, ?3, ?4)
		 ON CONFLICT(id) DO NOTHING`,
		accountID.String(), name, email, plan,
	)
	if err != nil {
		return issueKeyResponse{}, err
	}
	_, err = tx.Exec(
		`INSERT INTO api_keys
		   (id, account_id, name, api_key, description, creator_email, plan)
		 VALUES (?1, ?2, ?3, ?4, '', ?5, ?6)`,
		keyID.String(), accountID.String(), name, apiKey, email, plan,
	)
	if err != nil {
		return issueKeyResponse{}, err
	}
	if err = tx.Commit(); err != nil {
		return issueKeyResponse{}, err
	}
	return issueKeyResponse{
		APIKey:    apiKey,
		KeyID:     keyID.String(),
		AccountID: accountID.String(),
		Name:      name,
		Plan:      plan,
	}, nil
}

func ensureAccountAPIKey(accountID, name, email string) (models.APIKey, error) {
	key, err := loadAPIKeyByAccountID(accountID)
	if err == nil {
		return key, nil
	}
	parsed, err := uuid.Parse(accountID)
	if err != nil {
		return models.APIKey{}, err
	}
	resp, err := createAccountAPIKey(parsed, name, email, "free")
	if err != nil {
		return models.APIKey{}, err
	}
	return loadAPIKeyBySecret(resp.APIKey)
}

// generateAPIKey returns 64 hex chars of CSPRNG output.
func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
