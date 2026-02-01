package handlers

import (
	"bottrade/database"
	"bottrade/models"
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

	return c.Status(fiber.StatusCreated).JSON(models.RegisterBotResponse{
		BotID:           botID,
		APIKey:          apiKey,
		StartingBalance: 100000.00,
	})
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
