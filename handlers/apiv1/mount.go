// Package apiv1 implements the Benchmark API at /v1/* using huma.
//
// Design choice: we use huma instead of plain Fiber handlers because huma's
// type-first model GUARANTEES the OpenAPI spec, request validation, and
// the live behavior are always in sync. The Go request/response structs ARE
// the schema. There is no separately-maintained .yaml file that can drift.
//
// All operations live under /v1/* and require the RequireAPIKeyV1 middleware
// (huma-style) attached at Mount-time. The auto-generated docs are served at
// /docs (Swagger UI) and /openapi.json (machine-readable spec), both public.
package apiv1

import (
	"bottrade/services"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"
)

// Mount attaches the entire v1 API + its docs to the given Fiber app.
// Callers (main.go) provide an already-constructed ScenarioEngine; this
// function does not allocate one so the same engine instance can be shared
// with background jobs.
func Mount(app *fiber.App, engine *services.ScenarioEngine) {
	cfg := huma.DefaultConfig("BotTrade Benchmark API", "1.0.0")
	cfg.Info.Description = "" +
		"The BotTrade Benchmark API lets an external AI trading agent run " +
		"against frozen historical-bars scenarios and receive a graded " +
		"return. Agents loop: GET /v1/runs/{id}/market → POST /v1/runs/{id}/trades " +
		"→ POST /v1/runs/{id}/step → GET /v1/runs/{id}/results.\n\n" +
		"Auth: every /v1/* route requires `X-API-Key`. The simplest way to " +
		"get a key is `curl -X POST https://api.bot-trade.org/v1/keys` — no " +
		"body required, returns one immediately. Alternatively, register a " +
		"hosted bot at https://bot-trade.org/submit.\n\n" +
		"For a narrative onboarding guide, see /docs/agent.md."
	// Default Docs path is /docs; default OpenAPI path is /openapi.json.
	// We override the OpenAPI path to /docs/openapi.json so the public surface
	// is cleanly nested under /docs.
	cfg.OpenAPIPath = "/docs/openapi"

	h := &handlers{Engine: engine}

	// Public, no-auth fiber routes mounted BEFORE huma so they win the
	// route-table lookup for their exact paths:
	//   POST /v1/keys                  — self-serve key issuer
	//   GET  /v1/leaderboard           — public per-scenario ranking
	//   GET  /v1/leaderboard/scenarios — public scenario picker
	//   GET  /v1/runs/{id}/public      — read-only view of a published run
	h.mountKeyIssuer(app)
	h.mountLeaderboardPublic(app)
	h.mountPublicRun(app)

	api := humafiber.NewV2(app, cfg)
	api.UseMiddleware(h.authMiddleware(api))

	// Register all huma operations (these are X-API-Key-protected).
	h.registerScenarios(api)
	h.registerRuns(api)
	h.registerMarket(api)
	h.registerTrades(api)
	h.registerStep(api)
	h.registerResults(api)
	h.registerPublish(api)

	// Static docs that DON'T go through huma — served as plain markdown / text
	// at the fiber level so they bypass huma's content negotiation.
	h.mountStaticDocs(app)
}

// handlers carries the shared dependencies for every operation in the
// package. Methods on this type are the operation handlers themselves.
type handlers struct {
	Engine *services.ScenarioEngine
}
