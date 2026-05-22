package handlers

import "github.com/gofiber/fiber/v2"

// EmbedHeaders is middleware applied to /embed/* so the widget can be iframed
// from any third-party site. Without an explicit allow-list, any upstream
// CDN (Cloudflare, Railway) that defaults to X-Frame-Options: SAMEORIGIN
// breaks the embed silently.
func EmbedHeaders(c *fiber.Ctx) error {
	c.Set("X-Frame-Options", "ALLOWALL")
	c.Set("Content-Security-Policy", "frame-ancestors *")
	c.Set("Cache-Control", "public, max-age=300")
	return c.Next()
}
