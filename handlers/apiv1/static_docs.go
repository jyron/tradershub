package apiv1

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"
)

//go:embed agent.md
var agentDocMarkdown []byte

//go:embed agent-skills.md
var agentSkillsMarkdown []byte

//go:embed llms.txt
var llmsTxt []byte

//go:embed test_bot.py
var testBotPy []byte

//go:embed ai_bot.py
var aiBotPy []byte

// mountStaticDocs attaches the non-huma, public, no-auth doc routes
// directly on the Fiber app under /api/*. Docs must be readable without
// credentials.
func (h *handlers) mountStaticDocs(app *fiber.App) {
	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	app.Get("/api/agent.md", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/markdown; charset=utf-8")
		return c.Send(agentDocMarkdown)
	})

	app.Get("/api/agent-skills.md", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/markdown; charset=utf-8")
		return c.Send(agentSkillsMarkdown)
	})

	app.Get("/api/llms.txt", func(c *fiber.Ctx) error {
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
}
