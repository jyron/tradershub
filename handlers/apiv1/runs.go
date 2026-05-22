package apiv1

import (
	"bottrade/middleware"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
)

// Handlers carries the dependencies shared by every /v1/* handler. Wired
// at mount time so handlers themselves stay free of package globals.
type Handlers struct {
	Engine *services.ScenarioEngine
}

func NewHandlers(engine *services.ScenarioEngine) *Handlers {
	return &Handlers{Engine: engine}
}

// CreateRun starts a new run on a scenario.
//   POST /v1/runs   body: {scenario_id?: "...", scenario_slug?: "..."}
func (h *Handlers) CreateRun(c *fiber.Ctx) error {
	bot := middleware.GetBot(c)
	if bot.ID.String() == "" || bot.ID.String() == "00000000-0000-0000-0000-000000000000" {
		return jsonError(c, fiber.StatusUnauthorized, "unauthorized", "no bot in context")
	}

	var body struct {
		ScenarioID   string `json:"scenario_id"`
		ScenarioSlug string `json:"scenario_slug"`
	}
	if err := c.BodyParser(&body); err != nil {
		return jsonError(c, fiber.StatusBadRequest, "invalid_body", "could not parse request body")
	}

	scenarioID := body.ScenarioID
	if scenarioID == "" && body.ScenarioSlug != "" {
		// Resolve slug → id.
		if err := h.Engine.AppDB().QueryRow(
			`SELECT id FROM scenarios WHERE slug = ?1`, body.ScenarioSlug,
		).Scan(&scenarioID); err != nil {
			return jsonError(c, fiber.StatusNotFound, "scenario_not_found", "no scenario for that slug")
		}
	}
	if scenarioID == "" {
		return jsonError(c, fiber.StatusBadRequest, "missing_scenario", "scenario_id or scenario_slug required")
	}

	run, err := h.Engine.StartRun(bot.ID.String(), scenarioID)
	if err != nil {
		return jsonErrorf(c, fiber.StatusBadRequest, "start_run_failed", "%v", err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"run": run})
}

// GetRun returns the full snapshot (run + positions + queued orders + last equity).
//   GET /v1/runs/:id
func (h *Handlers) GetRun(c *fiber.Ctx) error {
	runID := c.Params("id")
	if err := h.assertRunOwner(c, runID); err != nil {
		return err
	}
	snap, err := h.Engine.GetRunState(runID)
	if err != nil {
		return jsonErrorf(c, fiber.StatusNotFound, "run_not_found", "%v", err)
	}
	return c.JSON(snap)
}

// assertRunOwner returns nil iff the authenticated bot owns the run.
func (h *Handlers) assertRunOwner(c *fiber.Ctx, runID string) error {
	bot := middleware.GetBot(c)
	var ownerID string
	err := h.Engine.AppDB().QueryRow(`SELECT bot_id FROM runs WHERE id = ?1`, runID).Scan(&ownerID)
	if err != nil {
		return jsonError(c, fiber.StatusNotFound, "run_not_found", "no such run")
	}
	if ownerID != bot.ID.String() {
		return jsonError(c, fiber.StatusForbidden, "run_not_owned", "you do not own this run")
	}
	return nil
}
