// Package apiv1 implements the Benchmark API at /v1/*.
//
// Conventions across all handlers in this package:
//   - Auth: middleware.RequireAPIKeyV1 (bot is in c.Locals("bot"))
//   - Errors: unified envelope {"error": {"code": "...", "message": "..."}}
//   - Success: handler-specific JSON body, HTTP 200 unless creating (201)
//   - Idempotency: writes (POST) accept optional idempotency_key in body
//     and round-trip identical responses on retry; mismatched body for the
//     same key → 409 conflict
package apiv1

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// jsonError writes a structured error response with the v1 envelope.
func jsonError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}

// jsonErrorf is a printf-style convenience.
func jsonErrorf(c *fiber.Ctx, status int, code, format string, args ...interface{}) error {
	return jsonError(c, status, code, fmt.Sprintf(format, args...))
}
