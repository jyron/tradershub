package apiv1

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"

	"bottrade/analytics"
)

//go:embed agent-skills.md
var agentSkillsMarkdown []byte

//go:embed llms.txt
var llmsTxt []byte

//go:embed test_bot.py
var testBotPy []byte

//go:embed ai_bot.py
var aiBotPy []byte

//go:embed ai_hedge_fund_adapter.py
var aiHedgeFundAdapterPy []byte

// captureDocView records an anonymous fetch of a public doc route. These are
// unauthenticated (often curl/agents), so the event is captured against the
// caller IP with $process_person_profile:false — counted, geo-resolved, but
// never creating a person profile.
func (h *handlers) captureDocView(c *fiber.Ctx, event string) {
	ip := clientIP(c)
	if ip == "" {
		ip = "docs_anon"
	}
	h.Analytics.Capture(ip, event, analytics.Props().
		Set("$process_person_profile", false).
		Set("$ip", ip).
		Set("user_agent", c.Get("User-Agent")))
}

// mountStaticDocs attaches the non-huma, public, no-auth doc routes
// directly on the Fiber app under /api/*. Docs must be readable without
// credentials.
func (h *handlers) mountStaticDocs(app *fiber.App) {
	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	// Domain-verification file for the official MCP Registry (HTTP auth).
	// Pairs with the Ed25519 signing key held outside the repo; the registry
	// fetches this to prove we control bot-trade.org for the org.bot-trade
	// namespace. Do not change without re-publishing the registry entry.
	app.Get("/.well-known/mcp-registry-auth", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.SendString("v=MCPv1; k=ed25519; p=sAlKM/EdO/oTvppIGYH38WOyvYpzUVNJlk5t0DUfq0g=")
	})

	app.Get("/api/agent-skills.md", func(c *fiber.Ctx) error {
		h.captureDocView(c, "agent_skills_viewed")
		c.Set("Content-Type", "text/markdown; charset=utf-8")
		return c.Send(agentSkillsMarkdown)
	})

	app.Get("/api/llms.txt", func(c *fiber.Ctx) error {
		h.captureDocView(c, "llms_txt_viewed")
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.Send(llmsTxt)
	})

	app.Get("/api/test_bot.py", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/x-python; charset=utf-8")
		return c.Send(testBotPy)
	})

	app.Get("/api/ai_bot.py", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/x-python; charset=utf-8")
		return c.Send(aiBotPy)
	})

	app.Get("/api/ai_hedge_fund_adapter.py", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/x-python; charset=utf-8")
		return c.Send(aiHedgeFundAdapterPy)
	})
}
