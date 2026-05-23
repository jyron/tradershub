// Package apiv1 implements the Benchmark API. All routes live under /api/*.
//
// Type-first via huma: the Go request/response structs ARE the OpenAPI
// schema, so the spec, validation, and live behavior cannot drift apart.
package apiv1

import (
	"bottrade/services"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"
)

// Mount attaches the API + its docs to the given Fiber app. Callers provide
// an already-constructed ScenarioEngine so the same engine instance is
// shared with background jobs.
func Mount(app *fiber.App, engine *services.ScenarioEngine) {
	cfg := huma.DefaultConfig("BotTrade Benchmark API", "1.0.0")
	cfg.Info.Description = "" +
		"The BotTrade Benchmark API lets an external trading agent run " +
		"against frozen historical-bars scenarios and receive a graded " +
		"return. Agents loop: GET /api/v1/runs/{id}/market → POST /api/v1/runs/{id}/trades " +
		"→ POST /api/v1/runs/{id}/step → GET /api/v1/runs/{id}/results.\n\n" +
		"Auth: most /api/v1/* routes require `X-API-Key`. Mint one with " +
		"`curl -X POST https://bot-trade.org/api/v1/keys` — no body, no signup. " +
		"GET /api/v1/scenarios and the leaderboard endpoints are public.\n\n" +
		"For a narrative integration guide see /api/agent.md."
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"

	h := &handlers{Engine: engine}

	// Public, no-auth fiber routes mounted BEFORE huma so they win the
	// route-table lookup for their exact paths.
	h.mountKeyIssuer(app)
	h.mountLeaderboardPublic(app)
	h.mountPublicRun(app)

	api := humafiber.NewV2(app, cfg)
	api.UseMiddleware(h.authMiddleware(api))

	h.registerScenarios(api)
	h.registerRuns(api)
	h.registerMarket(api)
	h.registerTrades(api)
	h.registerStep(api)
	h.registerResults(api)
	h.registerPublish(api)

	h.mountStaticDocs(app)
}

// handlers carries the shared dependencies for every operation in the
// package. Methods on this type are the operation handlers themselves.
type handlers struct {
	Engine *services.ScenarioEngine
}
