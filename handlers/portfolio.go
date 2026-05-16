package handlers

import (
	"bottrade/middleware"
	"bottrade/services"
	"database/sql"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// GetPortfolio returns the bot's portfolio. Defaults to the main account.
// Pass ?season_id=<uuid> to view the bot's isolated tournament account for
// that season.
func GetPortfolio(c *fiber.Ctx) error {
	botID := middleware.GetBotID(c)
	portfolioService := services.NewPortfolioService()

	if seasonIDStr := c.Query("season_id", ""); seasonIDStr != "" {
		seasonID, err := uuid.Parse(seasonIDStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid season_id",
			})
		}
		portfolio, err := portfolioService.GetSeasonPortfolio(botID, seasonID)
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "bot is not enrolled in this season",
			})
		}
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to load season portfolio: " + err.Error(),
			})
		}
		return c.JSON(portfolio)
	}

	portfolio, err := portfolioService.GetPortfolio(botID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch portfolio",
		})
	}
	return c.JSON(portfolio)
}
