package handlers

import (
	"bottrade/middleware"
	"bottrade/models"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
)

func TradeStock(c *fiber.Ctx) error {
	bot := middleware.GetBot(c)

	var req models.StockTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	tradingEngine := services.NewTradingEngine()
	trade, err := tradingEngine.ExecuteStockTrade(bot, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Broadcast trade to all WebSocket clients
	BroadcastEvent("trade", map[string]interface{}{
		"bot_id":    bot.ID,
		"bot_name":  bot.Name,
		"symbol":    trade.Symbol,
		"side":      trade.Side,
		"quantity":  trade.Quantity,
		"price":     trade.Price,
		"reasoning": trade.Reasoning,
		"timestamp": trade.ExecutedAt,
	})

	return c.Status(fiber.StatusOK).JSON(models.TradeResponse{
		TradeID:    trade.ID,
		Status:     "executed",
		Symbol:     trade.Symbol,
		Side:       trade.Side,
		Quantity:   trade.Quantity,
		Price:      trade.Price,
		Total:      trade.TotalValue,
		ExecutedAt: trade.ExecutedAt,
	})
}
