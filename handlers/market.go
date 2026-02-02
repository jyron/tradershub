package handlers

import (
	"bottrade/models"
	"bottrade/services"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetQuote(c *fiber.Ctx) error {
	symbol := strings.ToUpper(c.Params("symbol"))
	if symbol == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Symbol is required",
		})
	}

	marketService := services.GetMarketService()
	quote, err := marketService.GetQuote(symbol)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch quote",
		})
	}

	return c.JSON(quote)
}

func GetQuotes(c *fiber.Ctx) error {
	symbolsParam := c.Query("symbols")
	if symbolsParam == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Symbols parameter is required",
		})
	}

	symbols := strings.Split(symbolsParam, ",")
	var quotes []models.Quote

	marketService := services.GetMarketService()
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(strings.ToUpper(symbol))
		if symbol == "" {
			continue
		}

		quote, err := marketService.GetQuote(symbol)
		if err != nil {
			// Skip failed quotes
			continue
		}
		quotes = append(quotes, *quote)
	}

	return c.JSON(models.QuotesResponse{
		Quotes: quotes,
	})
}

func GetHistoricalCandles(c *fiber.Ctx) error {
	symbol := strings.ToUpper(c.Params("symbol"))
	if symbol == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Symbol is required",
		})
	}

	timeframe := c.Query("timeframe", "1Day")
	daysStr := c.Query("days", "30")

	var days int
	if _, err := fmt.Sscanf(daysStr, "%d", &days); err != nil || days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	end := time.Now()
	start := end.AddDate(0, 0, -days)

	alpacaClient := services.GetAlpacaClient()
	if alpacaClient == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Alpaca client not initialized",
		})
	}

	candles, err := alpacaClient.GetHistoricalCandles(symbol, timeframe, start, end)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch historical data",
		})
	}

	return c.JSON(fiber.Map{
		"symbol":    symbol,
		"timeframe": timeframe,
		"candles":   candles,
	})
}
