package handlers

import (
	"bottrade/middleware"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
)

func GetPortfolio(c *fiber.Ctx) error {
	botID := middleware.GetBotID(c)

	portfolioService := services.NewPortfolioService()
	portfolio, err := portfolioService.GetPortfolio(botID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch portfolio",
		})
	}

	return c.JSON(portfolio)
}
