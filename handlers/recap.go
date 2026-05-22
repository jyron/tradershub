package handlers

import (
	"bottrade/database"
	"database/sql"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// GET /api/recap/:date — date is YYYY-MM-DD. "latest" returns the most recent.
// Falls back to 404 when no recap exists.
func GetRecap(c *fiber.Ctx) error {
	date := c.Params("date", "latest")
	var (
		actualDate string
		payloadRaw string
		summary    string
		err        error
	)
	if date == "latest" {
		err = database.DB.QueryRow(`
			SELECT recap_date, payload, summary_md FROM daily_recaps
			ORDER BY recap_date DESC LIMIT 1`).
			Scan(&actualDate, &payloadRaw, &summary)
	} else {
		err = database.DB.QueryRow(`
			SELECT recap_date, payload, summary_md FROM daily_recaps
			WHERE recap_date = ?`, date).
			Scan(&actualDate, &payloadRaw, &summary)
	}
	if err == sql.ErrNoRows {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "no recap for that date",
		})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch recap",
		})
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "corrupt payload",
		})
	}
	return c.JSON(fiber.Map{
		"date":       actualDate,
		"summary_md": summary,
		"payload":    payload,
	})
}

// GET /api/recaps — list all available recap dates, newest first.
func ListRecaps(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT recap_date FROM daily_recaps ORDER BY recap_date DESC LIMIT 365`)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "list failed"})
	}
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			dates = append(dates, d)
		}
	}
	return c.JSON(fiber.Map{"dates": dates})
}
