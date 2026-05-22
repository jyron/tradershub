package apiv1

import "github.com/gofiber/fiber/v2"

// GetResults returns the computed metrics for a finished run. Computes on
// demand if no row exists yet (run just finished and worker hasn't picked
// it up). Errors out if the run is still active.
//   GET /v1/runs/:id/results
func (h *Handlers) GetResults(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}

	results, err := h.Engine.ComputeResults(runID)
	if err != nil {
		return jsonErrorf(c, fiber.StatusBadRequest, "results_failed", "%v", err)
	}
	return c.JSON(fiber.Map{"results": results})
}
