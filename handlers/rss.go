package handlers

import (
	"bottrade/database"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RSS 2.0 feeds for the three things people actually want notifications on:
// trades (per-bot or global), and daily recaps. Plain encoding/xml + struct
// tags — no deps.

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	LastBuild   string    `xml:"lastBuildDate"`
	Items       []rssItem `xml:"item"`
}
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

func origin(c *fiber.Ctx) string {
	scheme := "http"
	if c.Protocol() == "https" {
		scheme = "https"
	}
	return scheme + "://" + c.Hostname()
}

func writeRSS(c *fiber.Ctx, feed rssFeed) error {
	c.Set("Content-Type", "application/rss+xml; charset=utf-8")
	c.Set("Cache-Control", "public, max-age=300")
	out, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("encode failed")
	}
	return c.SendString(xml.Header + string(out))
}

// rfc1123Date converts the in-DB timestamps (RFC3339-ish or sqlite default
// "2006-01-02 15:04:05") to RFC1123Z, which RSS readers parse cleanly.
func rfc1123Date(s string) string {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC1123Z)
		}
	}
	return time.Now().UTC().Format(time.RFC1123Z)
}

// GET /rss/trades.xml — latest 100 trades across all active bots.
func RSSGlobalTrades(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT t.id, t.bot_id, COALESCE(b.name,'(unknown)'),
		       t.symbol, t.side, t.quantity, t.price, t.executed_at
		FROM trades t LEFT JOIN bots b ON b.id = t.bot_id
		WHERE t.season_id IS NULL
		ORDER BY t.executed_at DESC LIMIT 100`)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("query failed")
	}
	defer rows.Close()
	base := origin(c)
	items := make([]rssItem, 0, 100)
	for rows.Next() {
		var tradeID, botID, botName, symbol, action, executedAt string
		var quantity int
		var price float64
		if err := rows.Scan(&tradeID, &botID, &botName, &symbol, &action,
			&quantity, &price, &executedAt); err != nil {
			continue
		}
		items = append(items, rssItem{
			Title:       fmt.Sprintf("%s %s %d %s @ $%.2f", botName, action, quantity, symbol, price),
			Link:        fmt.Sprintf("%s/bots.html?id=%s", base, botID),
			GUID:        fmt.Sprintf("%s/trade/%s", base, tradeID),
			PubDate:     rfc1123Date(executedAt),
			Description: fmt.Sprintf("%s placed a %s of %d shares of %s at $%.2f.", botName, action, quantity, symbol, price),
		})
	}
	feed := rssFeed{Version: "2.0", Channel: rssChannel{
		Title: "bottrade · all trades", Link: base, Description: "Live AI trading bot trades from bottrade.",
		Language: "en", LastBuild: time.Now().UTC().Format(time.RFC1123Z), Items: items,
	}}
	return writeRSS(c, feed)
}

// GET /rss/bot/:id.xml — latest 100 trades for a specific bot.
func RSSBotTrades(c *fiber.Ctx) error {
	id := c.Params("id")
	id = strings.TrimSuffix(id, ".xml")
	botUUID, err := uuid.Parse(id)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid bot id")
	}
	var botName string
	err = database.DB.QueryRow(`SELECT name FROM bots WHERE id = ?`, botUUID.String()).Scan(&botName)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("bot not found")
	}
	rows, err := database.DB.Query(`
		SELECT id, symbol, side, quantity, price, executed_at
		FROM trades WHERE bot_id = ? AND season_id IS NULL
		ORDER BY executed_at DESC LIMIT 100`, botUUID.String())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("query failed")
	}
	defer rows.Close()
	base := origin(c)
	items := make([]rssItem, 0, 100)
	for rows.Next() {
		var tradeID, symbol, action, executedAt string
		var quantity int
		var price float64
		if err := rows.Scan(&tradeID, &symbol, &action, &quantity, &price, &executedAt); err != nil {
			continue
		}
		items = append(items, rssItem{
			Title:   fmt.Sprintf("%s %d %s @ $%.2f", action, quantity, symbol, price),
			Link:    fmt.Sprintf("%s/bots.html?id=%s", base, botUUID.String()),
			GUID:    fmt.Sprintf("%s/trade/%s", base, tradeID),
			PubDate: rfc1123Date(executedAt),
			Description: fmt.Sprintf("%s placed a %s of %d shares of %s at $%.2f.",
				botName, action, quantity, symbol, price),
		})
	}
	feed := rssFeed{Version: "2.0", Channel: rssChannel{
		Title:       fmt.Sprintf("bottrade · %s trades", botName),
		Link:        fmt.Sprintf("%s/bots.html?id=%s", base, botUUID.String()),
		Description: fmt.Sprintf("Latest trades from %s on bottrade.", botName),
		Language:    "en", LastBuild: time.Now().UTC().Format(time.RFC1123Z), Items: items,
	}}
	return writeRSS(c, feed)
}

// GET /rss/recaps.xml — daily recap summaries.
func RSSRecaps(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT recap_date, summary_md, created_at
		FROM daily_recaps ORDER BY recap_date DESC LIMIT 90`)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("query failed")
	}
	defer rows.Close()
	base := origin(c)
	items := make([]rssItem, 0, 90)
	for rows.Next() {
		var date, summary, createdAt string
		if err := rows.Scan(&date, &summary, &createdAt); err != nil {
			continue
		}
		items = append(items, rssItem{
			Title:       fmt.Sprintf("bottrade · %s recap", date),
			Link:        fmt.Sprintf("%s/today.html?date=%s", base, date),
			GUID:        fmt.Sprintf("%s/recap/%s", base, date),
			PubDate:     rfc1123Date(createdAt),
			Description: summary,
		})
	}
	feed := rssFeed{Version: "2.0", Channel: rssChannel{
		Title:       "bottrade · daily recap",
		Link:        base + "/today.html",
		Description: "Daily recap of AI trading bot activity on bottrade.",
		Language:    "en", LastBuild: time.Now().UTC().Format(time.RFC1123Z), Items: items,
	}}
	return writeRSS(c, feed)
}
