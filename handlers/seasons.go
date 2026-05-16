package handlers

import (
	"bottrade/middleware"
	"bottrade/models"
	"bottrade/services"
	"errors"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RequireAdminSecret gates admin-only routes. If ADMIN_SECRET is unset the
// route appears nonexistent (404) — we don't advertise admin surface to
// scanners. With it set, the X-Admin-Secret header must match exactly.
func RequireAdminSecret(c *fiber.Ctx) error {
	secret := os.Getenv("ADMIN_SECRET")
	if secret == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "not found"})
	}
	provided := c.Get("X-Admin-Secret")
	if provided == "" || provided != secret {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid admin credentials"})
	}
	return c.Next()
}

type CreateSeasonRequest struct {
	Name            string  `json:"name"`
	Slug            string  `json:"slug"`
	StartsAt        string  `json:"starts_at"` // RFC3339
	EndsAt          string  `json:"ends_at"`   // RFC3339
	StartingBalance float64 `json:"starting_balance"`
	AutoEnroll      *bool   `json:"auto_enroll"`
}

// ForceStartSeason flips a pending season to active immediately. Admin-only.
// Without this, dev cycles wait up to 5 min for SeasonManager to tick.
func ForceStartSeason(c *fiber.Ctx) error {
	id := c.Params("id")
	svc := services.NewSeasonService()
	sn, err := svc.GetSeason(id)
	if errors.Is(err, services.ErrSeasonNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "season not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load season"})
	}
	if err := svc.StartSeason(sn.ID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	out, _ := svc.GetSeason(sn.ID.String())
	return c.JSON(out)
}

// ForceCloseSeason flips an active season to closed immediately. Admin-only.
func ForceCloseSeason(c *fiber.Ctx) error {
	id := c.Params("id")
	svc := services.NewSeasonService()
	sn, err := svc.GetSeason(id)
	if errors.Is(err, services.ErrSeasonNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "season not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load season"})
	}
	if err := svc.CloseSeason(sn.ID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	out, _ := svc.GetSeason(sn.ID.String())
	return c.JSON(out)
}

// CreateSeason creates a pending season. Admin-only.
func CreateSeason(c *fiber.Ctx) error {
	var req CreateSeasonRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	if req.Name == "" || req.Slug == "" || req.StartsAt == "" || req.EndsAt == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "name, slug, starts_at, and ends_at are required",
		})
	}
	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "starts_at must be RFC3339"})
	}
	endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ends_at must be RFC3339"})
	}
	autoEnroll := false
	if req.AutoEnroll != nil {
		autoEnroll = *req.AutoEnroll
	}

	svc := services.NewSeasonService()
	sn, err := svc.CreateSeason(req.Name, req.Slug, startsAt, endsAt, req.StartingBalance, autoEnroll)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(sn)
}

// GetSeasons returns the season catalog. ?status= filters to a single status
// (pending|active|closed). Without it, all seasons are returned newest-first.
func GetSeasons(c *fiber.Ctx) error {
	status := c.Query("status", "")
	if status != "" && status != models.SeasonStatusPending &&
		status != models.SeasonStatusActive && status != models.SeasonStatusClosed {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid status; must be pending|active|closed",
		})
	}
	svc := services.NewSeasonService()
	seasons, err := svc.ListSeasons(status)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to load seasons",
		})
	}
	if seasons == nil {
		seasons = []models.Season{}
	}
	return c.JSON(fiber.Map{
		"seasons": seasons,
	})
}

// GetSeason returns one season by UUID or slug.
func GetSeason(c *fiber.Ctx) error {
	id := c.Params("id")
	svc := services.NewSeasonService()
	sn, err := svc.GetSeason(id)
	if errors.Is(err, services.ErrSeasonNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "season not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load season"})
	}
	return c.JSON(sn)
}

// GetSeasonLeaderboard returns live (or final) standings for the season.
func GetSeasonLeaderboard(c *fiber.Ctx) error {
	id := c.Params("id")
	svc := services.NewSeasonService()
	sn, err := svc.GetSeason(id)
	if errors.Is(err, services.ErrSeasonNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "season not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load season"})
	}
	_, entries, err := svc.Leaderboard(sn.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load leaderboard"})
	}
	if entries == nil {
		entries = []models.SeasonLeaderboardEntry{}
	}
	return c.JSON(fiber.Map{
		"season":   sn,
		"rankings": entries,
	})
}

// EnrollInSeason enrolls the authenticated bot. Closed seasons return 409,
// already-enrolled returns 409.
func EnrollInSeason(c *fiber.Ctx) error {
	bot := middleware.GetBot(c)
	id := c.Params("id")

	svc := services.NewSeasonService()
	sn, err := svc.GetSeason(id)
	if errors.Is(err, services.ErrSeasonNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "season not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load season"})
	}

	enrollment, err := svc.EnrollBot(sn.ID, bot.ID)
	if errors.Is(err, services.ErrSeasonClosed) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "season is closed"})
	}
	if errors.Is(err, services.ErrSeasonAlreadyActive) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "season has already started; enrollment is closed",
		})
	}
	if errors.Is(err, services.ErrAlreadyEnrolled) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "bot already enrolled in this season"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to enroll"})
	}
	return c.Status(fiber.StatusCreated).JSON(enrollment)
}
