package handlers

import (
	"bottrade/middleware"
	"bottrade/models"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// TradeStock executes a stock trade. If the request body includes season_id,
// the trade routes into the bot's isolated tournament account for that
// season; otherwise it hits the bot's main account.
func TradeStock(c *fiber.Ctx) error {
	bot := middleware.GetBot(c)

	var req models.StockTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	tradingEngine := services.NewTradingEngine()
	var (
		trade *models.Trade
		err   error
	)
	var seasonForBroadcast *uuid.UUID
	if req.SeasonID != "" {
		seasonID, parseErr := uuid.Parse(req.SeasonID)
		if parseErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid season_id",
			})
		}
		seasonForBroadcast = &seasonID
		trade, err = tradingEngine.ExecuteSeasonStockTrade(bot, seasonID, req)
	} else {
		trade, err = tradingEngine.ExecuteStockTrade(bot, req)
	}
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	event := map[string]interface{}{
		"bot_id":    bot.ID,
		"bot_name":  bot.Name,
		"symbol":    trade.Symbol,
		"side":      trade.Side,
		"quantity":  trade.Quantity,
		"price":     trade.Price,
		"reasoning": trade.Reasoning,
		"timestamp": trade.ExecutedAt,
	}
	if seasonForBroadcast != nil {
		event["season_id"] = seasonForBroadcast.String()
	}
	BroadcastEvent("trade", event)

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
