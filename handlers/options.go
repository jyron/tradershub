package handlers

import (
	"bottrade/middleware"
	"bottrade/models"
	"bottrade/services"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func GetOptionChain(c *fiber.Ctx) error {
	symbol := strings.ToUpper(c.Params("symbol"))
	if symbol == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Symbol is required",
		})
	}

	optionsService := services.NewOptionsService()
	contracts, err := optionsService.GetOptionChain(symbol)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(models.OptionChainResponse{
		UnderlyingSymbol: symbol,
		Contracts:        contracts,
	})
}

func TradeOption(c *fiber.Ctx) error {
	bot := middleware.GetBot(c)

	var req models.OptionTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	optionsService := services.NewOptionsService()
	trade, err := optionsService.ExecuteOptionTrade(bot, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	expDate := ""
	if trade.ExpirationDate != nil {
		expDate = trade.ExpirationDate.Format("2006-01-02")
	}

	strikePrice := 0.0
	if trade.StrikePrice != nil {
		strikePrice = *trade.StrikePrice
	}

	BroadcastEvent("trade", map[string]interface{}{
		"bot_id":          bot.ID,
		"bot_name":        bot.Name,
		"symbol":          trade.Symbol,
		"type":            trade.TradeType,
		"strike_price":    strikePrice,
		"expiration_date": expDate,
		"side":            trade.Side,
		"quantity":        trade.Quantity,
		"price":           trade.Price,
		"reasoning":       trade.Reasoning,
		"timestamp":       trade.ExecutedAt,
	})

	return c.Status(fiber.StatusOK).JSON(models.OptionTradeResponse{
		TradeID:          trade.ID.String(),
		Status:           "executed",
		Symbol:           trade.Symbol,
		UnderlyingSymbol: "",
		OptionType:       trade.TradeType,
		StrikePrice:      strikePrice,
		ExpirationDate:   expDate,
		Side:             trade.Side,
		Quantity:         trade.Quantity,
		Price:            trade.Price,
		Total:            trade.TotalValue,
		ExecutedAt:       trade.ExecutedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}
