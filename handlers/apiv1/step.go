package apiv1

import "github.com/gofiber/fiber/v2"

// Step advances the run's sim_time by N bars, filling queued orders.
//   POST /v1/runs/:id/step
//   body: {count?:1, idempotency_key?}
func (h *Handlers) Step(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}

	var body struct {
		Count          int    `json:"count"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = c.BodyParser(&body) // empty body is fine; defaults apply
	if body.Count == 0 {
		body.Count = 1
	}

	return withIdempotency(c, runID, body.IdempotencyKey, func() (int, interface{}, error) {
		result, err := h.Engine.AdvanceStep(runID, body.Count)
		if err != nil {
			return fiber.StatusBadRequest, fiber.Map{
				"error": fiber.Map{"code": "step_failed", "message": err.Error()},
			}, nil
		}
		return fiber.StatusOK, result, nil
	})
}
