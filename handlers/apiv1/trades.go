package apiv1

import (
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
)

// QueueTrade enqueues an order for fill on the next /step.
//   POST /v1/runs/:id/trades
//   body: {symbol, side, quantity, reasoning?, idempotency_key?}
func (h *Handlers) QueueTrade(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}

	var body struct {
		Symbol         string `json:"symbol"`
		Side           string `json:"side"`
		Quantity       int    `json:"quantity"`
		Reasoning      string `json:"reasoning"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := c.BodyParser(&body); err != nil {
		return jsonError(c, fiber.StatusBadRequest, "invalid_body", "could not parse request body")
	}

	return withIdempotency(c, runID, body.IdempotencyKey, func() (int, interface{}, error) {
		order, err := h.Engine.QueueTrade(runID, services.QueueTradeRequest{
			Symbol:    body.Symbol,
			Side:      body.Side,
			Quantity:  body.Quantity,
			Reasoning: body.Reasoning,
		})
		if err != nil {
			return fiber.StatusBadRequest, fiber.Map{
				"error": fiber.Map{"code": "queue_trade_failed", "message": err.Error()},
			}, nil
		}
		return fiber.StatusCreated, fiber.Map{"order": order}, nil
	})
}
