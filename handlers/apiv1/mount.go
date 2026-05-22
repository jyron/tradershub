package apiv1

import (
	"bottrade/middleware"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
)

// Mount attaches all /v1/* routes to the given Fiber app. Pass an engine
// constructed with NewScenarioEngine(appDB, marketDB) — this function does
// not allocate one itself so the caller controls lifecycle.
//
// Routes:
//   GET    /v1/scenarios
//   GET    /v1/scenarios/:id          (id or slug)
//   POST   /v1/runs
//   GET    /v1/runs/:id
//   GET    /v1/runs/:id/market
//   POST   /v1/runs/:id/trades
//   POST   /v1/runs/:id/step
//   GET    /v1/runs/:id/results
//   POST   /v1/runs/:id/publish
func Mount(app *fiber.App, engine *services.ScenarioEngine) {
	h := NewHandlers(engine)
	v1 := app.Group("/v1", middleware.RequireAPIKeyV1)

	v1.Get("/scenarios", h.ListScenarios)
	v1.Get("/scenarios/:id", h.GetScenario)

	v1.Post("/runs", h.CreateRun)
	v1.Get("/runs/:id", h.GetRun)
	v1.Get("/runs/:id/market", h.GetMarket)
	v1.Post("/runs/:id/trades", h.QueueTrade)
	v1.Post("/runs/:id/step", h.Step)
	v1.Get("/runs/:id/results", h.GetResults)
	v1.Post("/runs/:id/publish", h.Publish)
}
