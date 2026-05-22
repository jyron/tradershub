package handlers

import (
	"bottrade/database"
	"bottrade/services"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// OG cards are rendered on demand and cached for 5 minutes. The cards we
// generate are at most ~80 KB each; a tiny in-process LRU keeps the hot
// set warm without bloating memory.
const (
	ogCacheTTL     = 5 * time.Minute
	ogCacheMaxKeys = 200
)

type ogCacheEntry struct {
	png     []byte
	expires time.Time
}

type ogCacheT struct {
	mu sync.RWMutex
	m  map[string]ogCacheEntry
}

func (c *ogCacheT) get(k string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[k]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.png, true
}

func (c *ogCacheT) put(k string, png []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= ogCacheMaxKeys {
		// Cheap eviction: drop everything. The 5-minute TTL means churn is low.
		c.m = make(map[string]ogCacheEntry)
	}
	c.m[k] = ogCacheEntry{png: png, expires: time.Now().Add(ogCacheTTL)}
}

var ogCache = &ogCacheT{m: make(map[string]ogCacheEntry)}

func writePNG(c *fiber.Ctx, png []byte) error {
	c.Set("Content-Type", "image/png")
	c.Set("Cache-Control", "public, max-age=300, s-maxage=300")
	return c.Send(png)
}

// GET /og/bot/:id.png
func GetOGBot(c *fiber.Ctx) error {
	id := c.Params("id")
	id = trimPNGSuffix(id)
	botUUID, err := uuid.Parse(id)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid bot id")
	}
	cacheKey := "bot:" + botUUID.String()
	if png, ok := ogCache.get(cacheKey); ok {
		return writePNG(c, png)
	}

	var name, modelProvider, tier string
	var totalValue, pnl, pnlPct float64
	var tradeCount int
	err = database.DB.QueryRow(`
		SELECT b.name,
		       COALESCE(b.model_provider, ''),
		       COALESCE(NULLIF(b.tier,''), 'challenger') AS tier,
		       COALESCE((SELECT total_value FROM portfolio_snapshots
		                 WHERE bot_id = b.id AND season_id IS NULL
		                 ORDER BY snapshot_at DESC LIMIT 1), b.cash_balance) AS total_value,
		       COALESCE((SELECT COUNT(*) FROM trades WHERE bot_id = b.id AND season_id IS NULL), 0) AS trade_count
		FROM bots b WHERE b.id = ?`, botUUID.String()).
		Scan(&name, &modelProvider, &tier, &totalValue, &tradeCount)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("bot not found")
	}
	pnl = totalValue - 100000.0
	pnlPct = (pnl / 100000.0) * 100

	subtitle := tier
	if modelProvider != "" {
		subtitle = modelProvider + " · " + tier
	}

	in := services.OGCardInput{
		Kind:        "bot",
		Title:       name,
		Subtitle:    subtitle,
		BigMetric:   services.FormatPctSigned(pnlPct),
		BigPositive: pnlPct >= 0,
		Stats: []services.OGStat{
			{Label: "PORTFOLIO", Value: services.FormatMoney(totalValue)},
			{Label: "TRADES", Value: fmt.Sprintf("%d", tradeCount)},
			{Label: "P&L", Value: services.FormatMoney(pnl)},
		},
	}
	png, err := services.RenderOGCard(in)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("render failed")
	}
	ogCache.put(cacheKey, png)
	return writePNG(c, png)
}

// GET /og/leaderboard.png
func GetOGLeaderboard(c *fiber.Ctx) error {
	cacheKey := "leaderboard"
	if png, ok := ogCache.get(cacheKey); ok {
		return writePNG(c, png)
	}

	entries, err := loadEntries()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("leaderboard unavailable")
	}
	// Filter to verified+official, exclude baselines, sort by total value.
	filtered := entries[:0]
	for _, e := range entries {
		if e.IsBaseline {
			continue
		}
		if e.Tier != "verified" && e.Tier != "official" {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].TotalValue > filtered[j].TotalValue
	})
	if len(filtered) > 5 {
		filtered = filtered[:5]
	}

	rows := make([]services.OGRow, 0, len(filtered))
	for i, e := range filtered {
		rows = append(rows, services.OGRow{
			Rank:    i + 1,
			Name:    e.BotName,
			Metric:  services.FormatPctSigned(e.PnLPercent),
			Positiv: e.PnLPercent >= 0,
		})
	}

	in := services.OGCardInput{
		Kind:     "leaderboard",
		Title:    "Top AI Trading Bots",
		Subtitle: "Live benchmark · " + time.Now().UTC().Format("Jan 2, 2006"),
		Rows:     rows,
		Footer:   "bot-trade.org/leaderboard",
	}
	png, err := services.RenderOGCard(in)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("render failed")
	}
	ogCache.put(cacheKey, png)
	return writePNG(c, png)
}

// GET /og/trade/:id.png
func GetOGTrade(c *fiber.Ctx) error {
	id := c.Params("id")
	id = trimPNGSuffix(id)
	tradeUUID, err := uuid.Parse(id)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid trade id")
	}
	cacheKey := "trade:" + tradeUUID.String()
	if png, ok := ogCache.get(cacheKey); ok {
		return writePNG(c, png)
	}

	var botName, symbol, action string
	var quantity int
	var price float64
	var executedAt string
	err = database.DB.QueryRow(`
		SELECT b.name, t.symbol, t.side, t.quantity, t.price, t.executed_at
		FROM trades t JOIN bots b ON b.id = t.bot_id
		WHERE t.id = ?`, tradeUUID.String()).
		Scan(&botName, &symbol, &action, &quantity, &price, &executedAt)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("trade not found")
	}

	verb := "executed"
	switch action {
	case "buy":
		verb = "bought"
	case "sell":
		verb = "sold"
	}

	in := services.OGCardInput{
		Kind:        "trade",
		Title:       botName,
		Subtitle:    fmt.Sprintf("%s %d %s @ %s", verb, quantity, symbol, services.FormatMoney(price)),
		BigMetric:   symbol,
		BigPositive: action == "buy",
		Stats: []services.OGStat{
			{Label: "ACTION", Value: action},
			{Label: "SIZE", Value: fmt.Sprintf("%d shares", quantity)},
			{Label: "NOTIONAL", Value: services.FormatMoney(price * float64(quantity))},
		},
	}
	png, err := services.RenderOGCard(in)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("render failed")
	}
	ogCache.put(cacheKey, png)
	return writePNG(c, png)
}

func trimPNGSuffix(s string) string {
	if len(s) > 4 && s[len(s)-4:] == ".png" {
		return s[:len(s)-4]
	}
	return s
}
