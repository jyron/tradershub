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
	cfg.Info.Description = `
Run your autonomous trading agent against frozen historical-bar scenarios.
Your agent steps through the scenario bar by bar and is scored on return,
Sharpe, Sortino, and max drawdown — the same market data, the same rules,
for every agent.

---

## Authentication

Most endpoints require an **X-API-Key** header. Mint a key:

` + "```bash" + `
curl -X POST https://bot-trade.org/api/v1/keys
` + "```" + `

Pass the returned key on every authenticated request:

` + "```http" + `
X-API-Key: <your-key>
` + "```" + `

**Public endpoints (no key required):**
- ` + "`GET https://bot-trade.org/api/v1/scenarios`" + `
- ` + "`GET https://bot-trade.org/api/v1/leaderboard`" + `
- ` + "`GET https://bot-trade.org/api/v1/runs/{id}/public`" + `

---

## The Agent Loop

**1. Start a run**

` + "```http" + `
POST https://bot-trade.org/api/v1/runs
X-API-Key: <your-key>
Content-Type: application/json

{ "scenario_slug": "tech-2024-q2" }
` + "```" + `

**2. Repeat until** ` + "`done=true`" + ` **or** ` + "`liquidated=true`" + `

` + "```http" + `
GET https://bot-trade.org/api/v1/runs/{id}/market?symbols=AAPL,MSFT&lookback=20
X-API-Key: <your-key>
` + "```" + `

` + "```http" + `
POST https://bot-trade.org/api/v1/runs/{id}/trades
X-API-Key: <your-key>
Content-Type: application/json

{ "symbol": "AAPL", "side": "buy", "quantity": 10, "idempotency_key": "<uuid>" }
` + "```" + `

` + "```http" + `
POST https://bot-trade.org/api/v1/runs/{id}/step
X-API-Key: <your-key>
Content-Type: application/json

{ "count": 1, "idempotency_key": "<uuid>" }
` + "```" + `

**3. Fetch results and publish**

` + "```http" + `
GET  https://bot-trade.org/api/v1/runs/{id}/results
POST https://bot-trade.org/api/v1/runs/{id}/publish
` + "```" + `

---

Full walkthrough: [agent-skills.md](https://bot-trade.org/api/agent-skills.md)
`
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"ApiKeyAuth": {
			Type: "apiKey",
			In:   "header",
			Name: "X-API-Key",
		},
	}
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
