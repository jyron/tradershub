package handlers

import (
	"bottrade/database"
	"fmt"
	"html"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Twitter/Discord/Slack crawlers read og: / twitter:card meta tags from the
// initial HTML they receive — they don't run the SPA's JavaScript. To make
// shared bot links look good in previews, we intercept /bots.html and (when
// ?id= is present) inject per-bot meta tags into the static HTML before
// returning it.

var (
	botPageHTML     []byte
	botPageHTMLOnce bool
)

func readBotPage() ([]byte, error) {
	if botPageHTMLOnce {
		return botPageHTML, nil
	}
	b, err := os.ReadFile("./static/bots.html")
	if err != nil {
		return nil, err
	}
	botPageHTML = b
	botPageHTMLOnce = true
	return botPageHTML, nil
}

// BotPageMeta serves static/bots.html. If ?id= is present and resolves to a
// real bot, it injects og:image / twitter:image tags pointing at the /og/bot
// endpoint so the link preview is per-bot.
func BotPageMeta(c *fiber.Ctx) error {
	page, err := readBotPage()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("page unavailable")
	}

	idStr := c.Query("id", "")
	var botName, modelProvider string
	var pnlPct float64
	have := false
	if idStr != "" {
		if u, err := uuid.Parse(idStr); err == nil {
			var totalValue float64
			err := database.DB.QueryRow(`
				SELECT b.name,
				       COALESCE(b.model_provider,''),
				       COALESCE((SELECT total_value FROM portfolio_snapshots
				                 WHERE bot_id = b.id AND season_id IS NULL
				                 ORDER BY snapshot_at DESC LIMIT 1), b.cash_balance)
				FROM bots b WHERE b.id = ?`, u.String()).
				Scan(&botName, &modelProvider, &totalValue)
			if err == nil {
				pnlPct = ((totalValue - 100000.0) / 100000.0) * 100
				have = true
			}
		}
	}

	scheme := "http"
	if c.Protocol() == "https" {
		scheme = "https"
	}
	host := c.Hostname()

	var ogImage, ogTitle, ogDesc, ogURL string
	if have {
		ogTitle = fmt.Sprintf("%s · AI trading bot", botName)
		sign := "+"
		if pnlPct < 0 {
			sign = ""
		}
		provider := modelProvider
		if provider != "" {
			provider += " · "
		}
		ogDesc = fmt.Sprintf("%s%s%.2f%% portfolio return on bottrade — the AI trading benchmark.",
			provider, sign, pnlPct)
		ogImage = fmt.Sprintf("%s://%s/og/bot/%s.png", scheme, host, idStr)
		ogURL = fmt.Sprintf("%s://%s/bots.html?id=%s", scheme, host, idStr)
	} else {
		ogTitle = "bottrade · the AI trading benchmark"
		ogDesc = "Every frontier model trades the same $100k stock universe under the same rules. Submit yours."
		ogImage = fmt.Sprintf("%s://%s/og/leaderboard.png", scheme, host)
		ogURL = fmt.Sprintf("%s://%s/bots.html", scheme, host)
	}

	meta := buildMetaBlock(ogTitle, ogDesc, ogImage, ogURL)
	// Insert before </head>. Falls back to the original page if marker missing.
	idx := strings.Index(string(page), "</head>")
	if idx == -1 {
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Send(page)
	}
	out := make([]byte, 0, len(page)+len(meta))
	out = append(out, page[:idx]...)
	out = append(out, []byte(meta)...)
	out = append(out, page[idx:]...)
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(out)
}

func buildMetaBlock(title, desc, image, url string) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString("\n<!-- og meta injected server-side -->\n")
	b.WriteString(`<meta property="og:type" content="website" />` + "\n")
	b.WriteString(`<meta property="og:site_name" content="bottrade" />` + "\n")
	fmt.Fprintf(&b, "<meta property=\"og:title\" content=\"%s\" />\n", e(title))
	fmt.Fprintf(&b, "<meta property=\"og:description\" content=\"%s\" />\n", e(desc))
	fmt.Fprintf(&b, "<meta property=\"og:image\" content=\"%s\" />\n", e(image))
	fmt.Fprintf(&b, "<meta property=\"og:url\" content=\"%s\" />\n", e(url))
	b.WriteString(`<meta name="twitter:card" content="summary_large_image" />` + "\n")
	fmt.Fprintf(&b, "<meta name=\"twitter:title\" content=\"%s\" />\n", e(title))
	fmt.Fprintf(&b, "<meta name=\"twitter:description\" content=\"%s\" />\n", e(desc))
	fmt.Fprintf(&b, "<meta name=\"twitter:image\" content=\"%s\" />\n", e(image))
	return b.String()
}
