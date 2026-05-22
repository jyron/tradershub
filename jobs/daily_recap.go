package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// DailyRecapJob produces a "what happened today" summary once per US trading
// day. It runs every 30 minutes but no-ops unless:
//   - the local time in America/New_York is at or after market close (16:00),
//   - and no recap row exists for today's NYC date yet.
//
// This makes the trigger forgiving (a missed 21:00 cron doesn't break the
// next day's recap) without requiring real cron scheduling.
type DailyRecapJob struct{}

func NewDailyRecapJob() *DailyRecapJob {
	return &DailyRecapJob{}
}

func (j *DailyRecapJob) Name() string             { return "DailyRecap" }
func (j *DailyRecapJob) Interval() time.Duration  { return 30 * time.Minute }

// RecapPayload is what we persist in daily_recaps.payload and surface via
// /api/recap/:date. The schema is stable so RSS / static pages can consume
// it without a renderer round-trip.
type RecapPayload struct {
	Date         string         `json:"date"`
	TopMovers    []RecapMover   `json:"top_movers"`
	HotSymbols   []RecapSymbol  `json:"hot_symbols"`
	NewBotsToday int            `json:"new_bots_today"`
	BiggestTrade *RecapTrade    `json:"biggest_trade,omitempty"`
}
type RecapMover struct {
	BotID         string  `json:"bot_id"`
	BotName       string  `json:"bot_name"`
	ModelProvider string  `json:"model_provider,omitempty"`
	// PnLPercent here is *today's* move (latest snapshot vs. last pre-midnight
	// snapshot), not cumulative-since-inception PnL. The field name stays
	// pnl_percent for frontend compatibility but the semantics are intraday.
	PnLPercent float64 `json:"pnl_percent"`
	TotalValue float64 `json:"total_value"`
}
type RecapSymbol struct {
	Symbol     string `json:"symbol"`
	TradeCount int    `json:"trade_count"`
}
type RecapTrade struct {
	TradeID    string  `json:"trade_id"`
	BotID      string  `json:"bot_id"`
	BotName    string  `json:"bot_name"`
	Symbol     string  `json:"symbol"`
	Action     string  `json:"action"`
	Quantity   int     `json:"quantity"`
	Price      float64 `json:"price"`
	Notional   float64 `json:"notional"`
	ExecutedAt string  `json:"executed_at"`
}

func (j *DailyRecapJob) Run() error {
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("load NYC tz: %w", err)
	}
	now := time.Now().In(nyc)
	// Don't generate before market close — we want the day's trades to be in.
	if now.Hour() < 16 {
		return nil
	}
	dateKey := now.Format("2006-01-02")

	var exists int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM daily_recaps WHERE recap_date = ?`, dateKey).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check existing recap: %w", err)
	}
	if exists > 0 {
		return nil
	}

	payload, err := buildRecapPayload(dateKey, nyc)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	summary := generateRecapSummary(payload)

	_, err = database.DB.Exec(`
		INSERT INTO daily_recaps (recap_date, payload, summary_md, created_at)
		VALUES (?, ?, ?, ?)`,
		dateKey, string(payloadJSON), summary, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert recap: %w", err)
	}
	log.Printf("DailyRecap: wrote %s (movers=%d, hot=%d, new_bots=%d)",
		dateKey, len(payload.TopMovers), len(payload.HotSymbols), payload.NewBotsToday)
	return nil
}

func buildRecapPayload(dateKey string, nyc *time.Location) (*RecapPayload, error) {
	p := &RecapPayload{Date: dateKey}

	// Day boundary in NYC, expressed as UTC for the WHERE clause.
	dayStart, _ := time.ParseInLocation("2006-01-02", dateKey, nyc)
	dayEnd := dayStart.Add(24 * time.Hour)
	dayStartUTC := dayStart.UTC().Format("2006-01-02 15:04:05")
	dayEndUTC := dayEnd.UTC().Format("2006-01-02 15:04:05")

	// Top movers: today's intraday move per bot, NOT cumulative-since-inception.
	// "Today's move" = (latest snapshot value) - (most recent snapshot taken
	// before dayStartUTC). The portfolio-snapshot job runs hourly so a
	// pre-midnight-NYC snapshot exists for any verified/official bot.
	rows, err := database.DB.Query(`
		SELECT b.id, b.name, COALESCE(b.model_provider,''),
		       COALESCE((SELECT total_value FROM portfolio_snapshots
		                 WHERE bot_id = b.id AND season_id IS NULL
		                 ORDER BY snapshot_at DESC LIMIT 1), b.cash_balance) AS latest_value,
		       (SELECT total_value FROM portfolio_snapshots
		        WHERE bot_id = b.id AND season_id IS NULL AND snapshot_at < ?
		        ORDER BY snapshot_at DESC LIMIT 1) AS prior_close
		FROM bots b
		WHERE b.is_active = 1
		  AND COALESCE(b.tier,'') IN ('verified','official')
		  AND COALESCE(b.is_baseline,0) = 0`, dayStartUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type bot struct {
		id, name, provider string
		latest             float64
		priorClose         sql.NullFloat64
	}
	var bots []bot
	for rows.Next() {
		var b bot
		if err := rows.Scan(&b.id, &b.name, &b.provider, &b.latest, &b.priorClose); err == nil {
			bots = append(bots, b)
		}
	}
	type scored struct {
		b   bot
		pct float64
	}
	scoreds := make([]scored, 0, len(bots))
	for _, b := range bots {
		// If we have no prior-close snapshot (a brand-new bot registered today),
		// skip — we can't meaningfully claim a "today's move" for it. It'll
		// show up tomorrow.
		if !b.priorClose.Valid || b.priorClose.Float64 <= 0 {
			continue
		}
		pct := ((b.latest - b.priorClose.Float64) / b.priorClose.Float64) * 100
		scoreds = append(scoreds, scored{b: b, pct: pct})
	}
	for i := 0; i < len(scoreds); i++ {
		for k := i + 1; k < len(scoreds); k++ {
			if absF(scoreds[k].pct) > absF(scoreds[i].pct) {
				scoreds[i], scoreds[k] = scoreds[k], scoreds[i]
			}
		}
	}
	for i := 0; i < len(scoreds) && i < 3; i++ {
		s := scoreds[i]
		p.TopMovers = append(p.TopMovers, RecapMover{
			BotID:         s.b.id,
			BotName:       s.b.name,
			ModelProvider: s.b.provider,
			PnLPercent:    s.pct,
			TotalValue:    s.b.latest,
		})
	}

	// Hot symbols: trades today, grouped by symbol.
	hotRows, err := database.DB.Query(`
		SELECT symbol, COUNT(*) AS c
		FROM trades
		WHERE season_id IS NULL
		  AND executed_at >= ? AND executed_at < ?
		GROUP BY symbol ORDER BY c DESC LIMIT 5`,
		dayStartUTC, dayEndUTC)
	if err == nil {
		defer hotRows.Close()
		for hotRows.Next() {
			var r RecapSymbol
			if err := hotRows.Scan(&r.Symbol, &r.TradeCount); err == nil {
				p.HotSymbols = append(p.HotSymbols, r)
			}
		}
	}

	// New bots registered today.
	_ = database.DB.QueryRow(`
		SELECT COUNT(*) FROM bots WHERE created_at >= ? AND created_at < ?`,
		dayStartUTC, dayEndUTC).Scan(&p.NewBotsToday)

	// Biggest trade today (by notional).
	var t RecapTrade
	err = database.DB.QueryRow(`
		SELECT t.id, t.bot_id, b.name, t.symbol, t.side, t.quantity, t.price, t.executed_at
		FROM trades t JOIN bots b ON b.id = t.bot_id
		WHERE t.season_id IS NULL
		  AND t.executed_at >= ? AND t.executed_at < ?
		ORDER BY (t.quantity * t.price) DESC LIMIT 1`,
		dayStartUTC, dayEndUTC).
		Scan(&t.TradeID, &t.BotID, &t.BotName, &t.Symbol, &t.Action, &t.Quantity, &t.Price, &t.ExecutedAt)
	if err == nil {
		t.Notional = float64(t.Quantity) * t.Price
		p.BiggestTrade = &t
	} else if err != sql.ErrNoRows {
		log.Printf("DailyRecap: biggest-trade query: %v", err)
	}

	return p, nil
}

// generateRecapSummary asks Claude for a 2-paragraph blurb. If the API key
// isn't configured, the gate env isn't set, or the call fails, returns a
// deterministic template so the recap row still goes in.
//
// BOTTRADE_ENABLE_RECAP_LLM must be "1" to actually call Anthropic — without
// the gate, local/CI smokes that happen to have ANTHROPIC_API_KEY in the
// env would silently bill on every restart.
func generateRecapSummary(p *RecapPayload) string {
	if os.Getenv("BOTTRADE_ENABLE_RECAP_LLM") == "1" && services.AnthropicKey() != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		facts, _ := json.MarshalIndent(p, "", "  ")
		sysPrompt := "You write daily one- to two-paragraph recaps of an AI trading bot benchmark site (bottrade). " +
			"Tone: punchy, observational, lightly playful. No financial advice. Mention bots by name. " +
			"Do not use the word 'today' more than once. Do not introduce new facts beyond the JSON I provide."
		usrPrompt := "Here's today's tape, as JSON. Write the recap.\n\n" + string(facts)
		text, err := services.AnthropicComplete(ctx, "claude-sonnet-4-6", sysPrompt, usrPrompt, 600)
		if err == nil && strings.TrimSpace(text) != "" {
			return text
		}
		log.Printf("DailyRecap: Anthropic failed, falling back to template: %v", err)
	}
	return templateSummary(p)
}

func templateSummary(p *RecapPayload) string {
	var b strings.Builder
	if len(p.TopMovers) > 0 {
		lead := p.TopMovers[0]
		dir := "leads the board"
		if lead.PnLPercent < 0 {
			dir = "is hurting"
		}
		fmt.Fprintf(&b, "**%s** %s with %+.2f%% on a %s portfolio. ",
			lead.BotName, dir, lead.PnLPercent, formatMoneyShort(lead.TotalValue))
		if len(p.TopMovers) > 1 {
			fmt.Fprintf(&b, "Behind: ")
			for i, m := range p.TopMovers[1:] {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s (%+.2f%%)", m.BotName, m.PnLPercent)
			}
			b.WriteString(". ")
		}
	}
	if len(p.HotSymbols) > 0 {
		fmt.Fprintf(&b, "\n\nMost-traded names: ")
		for i, s := range p.HotSymbols {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s (%d)", s.Symbol, s.TradeCount)
		}
		b.WriteString(".")
	}
	if p.NewBotsToday > 0 {
		fmt.Fprintf(&b, " %d new bot%s entered the arena.",
			p.NewBotsToday, plural(p.NewBotsToday))
	}
	if b.Len() == 0 {
		return "Quiet session — no qualifying trades or movers."
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func formatMoneyShort(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("$%.2fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("$%.1fk", v/1_000)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}
