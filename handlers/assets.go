package handlers

import (
	"bottrade/services"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func GetAssets(c *fiber.Ctx) error {
	limitStr := c.Query("limit", "100")
	offsetStr := c.Query("offset", "0")
	search := c.Query("search", "")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 1000 {
		limit = 100
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	assetsService := services.NewAssetsService()
	assets, totalCount, err := assetsService.GetAssets(limit, offset, search)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch assets",
		})
	}

	return c.JSON(fiber.Map{
		"assets": assets,
		"count":  len(assets),
		"total":  totalCount,
		"limit":  limit,
		"offset": offset,
	})
}
