package handlers

import (
	"bottrade/database"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// validTiers is the allow-list for the admin tier-promotion endpoint.
// "baseline" is intentionally NOT a tier — baseline bots live at
// tier='official' with is_baseline=1.
var validTiers = map[string]bool{
	"challenger": true,
	"verified":   true,
	"official":   true,
}

type updateTierRequest struct {
	Tier string `json:"tier"`
}

// PromoteBotTier — POST /api/admin/bots/:id/tier
// Body: {"tier":"verified"}
// Admin-only; used to hand-promote a verified bot to official (frontier
// curation) or to demote a misbehaving bot back to challenger.
func PromoteBotTier(c *fiber.Ctx) error {
	idStr := c.Params("id")
	botID, err := uuid.Parse(idStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid bot id"})
	}

	var req updateTierRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	if !validTiers[req.Tier] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "tier must be one of: challenger, verified, official",
		})
	}

	res, err := database.DB.Exec(
		`UPDATE bots SET tier = ? WHERE id = ?`,
		req.Tier, botID.String(),
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to update tier",
			"details": err.Error(),
		})
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Bot not found"})
	}

	// Invalidate the leaderboard cache so the change is visible immediately
	// — without this, a freshly-promoted bot waits up to 30s to appear.
	leaderboardCache.clear()

	return c.JSON(fiber.Map{
		"success": true,
		"bot_id":  botID.String(),
		"tier":    req.Tier,
	})
}
