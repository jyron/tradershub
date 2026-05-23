package apiv1

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"
)

// agentDocMarkdown is the full integration guide for AI / external bots.
// Served raw as text/markdown — no rendering, no auth.
//
//go:embed agent.md
var agentDocMarkdown []byte

// llmsTxt is the LLM-discovery file modeled on the proposed llms.txt
// convention (https://llmstxt.org). Lives at the root so an agent fetching
// "/llms.txt" finds the map of useful resources.
//
//go:embed llms.txt
var llmsTxt []byte

// testBotPy is the reference test bot for the Benchmark API. Served raw at
// /docs/test_bot.py so a user can `curl > test_bot.py && python test_bot.py`.
// Read agent.md for the rationale — this file is the canonical "how to use
// the API correctly" example.
//
//go:embed test_bot.py
var testBotPy []byte

// mountStaticDocs attaches the non-huma, public, no-auth doc routes
// directly on the Fiber app. These deliberately sit OUTSIDE huma so they
// don't go through the X-API-Key middleware — docs must be readable
// without credentials.
func (h *handlers) mountStaticDocs(app *fiber.App) {
	// Friendly root + healthcheck for ops and curious humans.
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name":        "BotTrade Benchmark API",
			"version":     "v1",
			"docs":        "/docs",
			"agent_guide": "/docs/agent.md",
			"openapi":     "/docs/openapi.json",
			"test_bot":    "/docs/test_bot.py",
			"llms_txt":    "/llms.txt",
			"site":        "https://bot-trade.org",
		})
	})
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	// Agent integration guide — raw markdown so agents and CLIs can read
	// it without rendering. text/markdown is the correct content type per
	// the IETF draft (it's also what GitHub/GitLab use for raw .md).
	app.Get("/docs/agent.md", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/markdown; charset=utf-8")
		return c.Send(agentDocMarkdown)
	})

	// llms.txt — also markdown but served at the root with text/plain per
	// the convention (so a curl with no Accept header gets readable text).
	app.Get("/llms.txt", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.Send(llmsTxt)
	})

	// Reference test bot — served as plain text so `curl > test_bot.py`
	// gives a runnable Python file.
	app.Get("/docs/test_bot.py", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/x-python; charset=utf-8")
		return c.Send(testBotPy)
	})
}
