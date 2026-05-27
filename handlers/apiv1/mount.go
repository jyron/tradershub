// Package apiv1 implements the Benchmark API. All routes live under /api/*.
//
// Type-first via huma: the Go request/response structs ARE the OpenAPI
// schema, so the spec, validation, and live behavior cannot drift apart.
package apiv1

import (
	"bottrade/config"
	"bottrade/services"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"
)

// Mount attaches the API + its docs to the given Fiber app. Callers provide
// an already-constructed ScenarioEngine so the same engine instance is
// shared with background jobs.
func Mount(app *fiber.App, engine *services.ScenarioEngine, appCfg *config.Config) {
	cfg := huma.DefaultConfig("BotTrade Benchmark API", "1.0.0")
	cfg.Info.Description = `
Run your autonomous trading agent against frozen historical-bar scenarios.
Your agent steps through the scenario bar by bar and is scored on return,
Sharpe, Sortino, and max drawdown. Each run is pinned to a scenario version,
uses deterministic execution rules, and can be published to the leaderboard
when you want the result to be public.

---

## Authentication

Most endpoints require a BotTrade API key. Sign in to get your key:

https://bot-trade.org/account

Pass the returned key on every authenticated request. Both forms work:

` + "```http" + `
X-API-Key: <your-key>
Authorization: Bearer <your-key>
` + "```" + `

The key authenticates to a BotTrade account. The account owns plan, quota,
billing, runs, and public leaderboard identity.

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
		"BearerAuth": {
			Type:   "http",
			Scheme: "bearer",
		},
	}
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"

	h := &handlers{
		Engine:              engine,
		StripeSecretKey:     appCfg.StripeSecretKey,
		StripeWebhookSecret: appCfg.StripeWebhookSecret,
		StripeProPriceID:    appCfg.StripeProPriceID,
		AppBaseURL:          appCfg.AppBaseURL,
		GoogleClientID:      appCfg.GoogleOAuthClientID,
		GoogleClientSecret:  appCfg.GoogleOAuthClientSecret,
		GitHubClientID:      appCfg.GitHubOAuthClientID,
		GitHubClientSecret:  appCfg.GitHubOAuthClientSecret,
	}

	// Public, no-auth fiber routes mounted BEFORE huma so they win the
	// route-table lookup for their exact paths.
	h.mountKeyIssuer(app)
	h.mountLeaderboardPublic(app)
	h.mountPublicRun(app)
	h.mountBillingWebhook(app)
	h.mountOAuth(app)

	api := humafiber.NewV2(app, cfg)
	api.UseMiddleware(h.authMiddleware(api))

	h.registerScenarios(api)
	h.registerRuns(api)
	h.registerMarket(api)
	h.registerTrades(api)
	h.registerStep(api)
	h.registerResults(api)
	h.registerPublish(api)
	h.registerBilling(api)

	h.mountStaticDocs(app)
}

// handlers carries the shared dependencies for every operation in the
// package. Methods on this type are the operation handlers themselves.
type handlers struct {
	Engine              *services.ScenarioEngine
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeProPriceID    string
	AppBaseURL          string
	GoogleClientID      string
	GoogleClientSecret  string
	GitHubClientID      string
	GitHubClientSecret  string
}
