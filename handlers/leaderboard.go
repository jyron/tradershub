package handlers

import (
	"bottrade/database"
	"bottrade/services"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// startingBalance is the cash every bot is seeded with. Used to normalize
// returns into percentages.
const startingBalance = 100000.0

// MinTradesForRanking is the eligibility floor for risk-adjusted boards.
// Without it, a bot with one lucky trade can post a perfect Sharpe and top
// the list — destroying credibility. Five is small enough that a real bot
// clears it in an hour of trading and large enough that pure-luck winners
// are rare.
const MinTradesForRanking = 5

// LeaderboardEntry is one row of the per-bot leaderboard. All risk-adjusted
// metrics return nil (omitted) when there isn't enough history. Eligible is
// false when the bot fails to meet MinTradesForRanking or hasn't accumulated
// enough snapshots — in either case it is excluded from risk-adjusted views
// and from creator averages.
type LeaderboardEntry struct {
	Rank          int      `json:"rank"`
	BotID         string   `json:"bot_id"`
	BotName       string   `json:"bot_name"`
	CreatorEmail  string   `json:"creator_email,omitempty"`
	TotalValue    float64  `json:"total_value"`
	PnL           float64  `json:"pnl"`
	PnLPercent    float64  `json:"pnl_percent"`
	TradeCount    int      `json:"trade_count"`
	Sharpe        *float64 `json:"sharpe,omitempty"`
	Sortino       *float64 `json:"sortino,omitempty"`
	MaxDrawdown   *float64 `json:"max_drawdown,omitempty"`
	SnapshotCount int      `json:"snapshot_count"`
	Eligible      bool     `json:"eligible"`
	RankChange    int      `json:"rank_change"`
	PreviousRank  int      `json:"previous_rank"`
}

// CreatorEntry rolls every bot under one creator_email into a single row. The
// purpose is anti-Sybil: someone running 50 random bots can no longer brag
// about their one lucky winner — the creator-level number averages the field.
type CreatorEntry struct {
	Rank            int     `json:"rank"`
	CreatorEmail    string  `json:"creator_email"`
	BotCount        int     `json:"bot_count"`
	TotalValue      float64 `json:"total_value"`        // summed across bots
	AvgPnLPercent   float64 `json:"avg_pnl_percent"`    // average return per bot
	BestBotPnLPct   float64 `json:"best_bot_pnl_percent"`
	WorstBotPnLPct  float64 `json:"worst_bot_pnl_percent"`
	TotalTradeCount int     `json:"total_trade_count"`
}

type LeaderboardResponse struct {
	Period             string             `json:"period"`
	SortBy             string             `json:"sort_by"`
	Rankings           []LeaderboardEntry `json:"rankings"`
	Creators           []CreatorEntry     `json:"creators,omitempty"`
	HiddenIneligible   int                `json:"hidden_ineligible"`
	MinTradesRequired  int                `json:"min_trades_required"`
}

// GetLeaderboard returns multiple sortable views in a single payload. The
// frontend picks which one to show. ?sort_by=value|sharpe|sortino|drawdown
// controls the rank assigned on the per-bot list. The creators array is
// included unconditionally so the frontend can flip tabs without re-fetching.
func GetLeaderboard(c *fiber.Ctx) error {
	period := c.Query("period", "all")
	sortBy := strings.ToLower(c.Query("sort_by", "value"))

	limit := 50
	if _, err := fmt.Sscanf(c.Query("limit", "50"), "%d", &limit); err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	entries, err := loadEntries()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch leaderboard",
		})
	}

	// Risk-adjusted views filter out ineligible bots entirely. Total Value
	// shows everyone, since that's the "fun" headline number and exclusion
	// would make the page look empty on a fresh deploy.
	visible := entries
	hidden := 0
	if sortBy != "value" {
		filtered := visible[:0]
		for _, e := range entries {
			if e.Eligible {
				filtered = append(filtered, e)
			} else {
				hidden++
			}
		}
		visible = filtered
	}

	sortEntries(visible, sortBy)
	if len(visible) > limit {
		visible = visible[:limit]
	}

	applyRankMovement(visible)
	if sortBy == "value" {
		persistTodaysRanking(visible)
	}

	creators := buildCreatorRankings(entries)

	return c.JSON(LeaderboardResponse{
		Period:            period,
		SortBy:            sortBy,
		Rankings:          visible,
		Creators:          creators,
		HiddenIneligible:  hidden,
		MinTradesRequired: MinTradesForRanking,
	})
}

func loadEntries() ([]LeaderboardEntry, error) {
	rows, err := database.DB.Query(`
		SELECT
			b.id,
			b.name,
			COALESCE(b.creator_email, ''),
			COALESCE(COUNT(t.id), 0) AS trade_count
		FROM bots b
		LEFT JOIN trades t ON b.id = t.bot_id AND t.season_id IS NULL
		WHERE b.is_active = 1
		GROUP BY b.id, b.name, b.creator_email
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	portfolioService := services.NewPortfolioService()
	var entries []LeaderboardEntry

	for rows.Next() {
		var idStr, name, email string
		var tradeCount int
		if err := rows.Scan(&idStr, &name, &email, &tradeCount); err != nil {
			continue
		}
		botID, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		portfolio, err := portfolioService.GetPortfolio(botID)
		if err != nil {
			continue
		}

		entry := LeaderboardEntry{
			BotID:        botID.String(),
			BotName:      name,
			CreatorEmail: email,
			TotalValue:   portfolio.TotalValue,
			PnL:          portfolio.TotalPnL,
			PnLPercent:   portfolio.TotalPnLPct,
			TradeCount:   tradeCount,
		}

		values := loadSnapshotValues(botID)
		entry.SnapshotCount = len(values)
		metrics := services.ComputeMetrics(values)
		// Eligibility requires *both* a real track record (enough trades to
		// not be a one-shot lucky run) and enough datapoints for the math
		// to mean anything.
		entry.Eligible = tradeCount >= MinTradesForRanking && metrics.Valid
		if entry.Eligible {
			s, so, dd := metrics.Sharpe, metrics.Sortino, metrics.MaxDrawdown
			entry.Sharpe = &s
			entry.Sortino = &so
			entry.MaxDrawdown = &dd
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func loadSnapshotValues(botID uuid.UUID) []float64 {
	rows, err := database.DB.Query(
		`SELECT total_value FROM portfolio_snapshots
		 WHERE bot_id = ?1 AND season_id IS NULL
		 ORDER BY snapshot_at ASC`,
		botID.String(),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var values []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err == nil {
			values = append(values, v)
		}
	}
	return values
}

// sortEntries orders the slice based on the requested view. Entries missing a
// risk-adjusted metric are pushed to the bottom rather than sorted as zero.
func sortEntries(entries []LeaderboardEntry, sortBy string) {
	switch sortBy {
	case "sharpe":
		sort.SliceStable(entries, func(i, j int) bool {
			return rankNullableDesc(entries[i].Sharpe, entries[j].Sharpe)
		})
	case "sortino":
		sort.SliceStable(entries, func(i, j int) bool {
			return rankNullableDesc(entries[i].Sortino, entries[j].Sortino)
		})
	case "drawdown":
		// Lower drawdown is better, so we flip the comparison.
		sort.SliceStable(entries, func(i, j int) bool {
			return rankNullableAsc(entries[i].MaxDrawdown, entries[j].MaxDrawdown)
		})
	default: // "value"
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].TotalValue > entries[j].TotalValue
		})
	}
}

func rankNullableDesc(a, b *float64) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a > *b
}

func rankNullableAsc(a, b *float64) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

func applyRankMovement(entries []LeaderboardEntry) {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	previousRanks := make(map[string]int)
	rows, err := database.DB.Query(`
		SELECT bot_id, rank
		FROM ranking_snapshots
		WHERE snapshot_date = ?
	`, yesterday)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var botID string
			var rank int
			if err := rows.Scan(&botID, &rank); err == nil {
				previousRanks[botID] = rank
			}
		}
	}
	for i := range entries {
		entries[i].Rank = i + 1
		if prev, ok := previousRanks[entries[i].BotID]; ok {
			entries[i].PreviousRank = prev
			entries[i].RankChange = prev - entries[i].Rank
		}
	}
}

// persistTodaysRanking writes ranking_snapshots only when entries are sorted
// by total value — that's the canonical leaderboard rank used for the daily
// rank-change indicator.
func persistTodaysRanking(entries []LeaderboardEntry) {
	today := time.Now().Format("2006-01-02")
	for _, e := range entries {
		botUUID, err := uuid.Parse(e.BotID)
		if err != nil {
			continue
		}
		database.DB.Exec(`
			INSERT INTO ranking_snapshots (id, bot_id, rank, total_value, snapshot_date)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (bot_id, snapshot_date)
			DO UPDATE SET rank = excluded.rank, total_value = excluded.total_value
		`, uuid.NewString(), botUUID.String(), e.Rank, e.TotalValue, today)
	}
}

// buildCreatorRankings groups bots by email and ranks creators by average
// per-bot return. Bots with no email (legacy / unclaimed) are excluded from
// this view so they can't be falsely associated with a creator.
func buildCreatorRankings(entries []LeaderboardEntry) []CreatorEntry {
	type bucket struct {
		bots       int
		totalValue float64
		sumPct     float64
		bestPct    float64
		worstPct   float64
		trades     int
		seenAny    bool
	}
	groups := make(map[string]*bucket)
	for _, e := range entries {
		if e.CreatorEmail == "" {
			continue
		}
		// Anti-sybil: only eligible bots contribute. A creator who registers
		// 50 random bots can't dilute their score with 49 untested ones,
		// because untested bots don't count toward the average at all.
		if !e.Eligible {
			continue
		}
		b, ok := groups[e.CreatorEmail]
		if !ok {
			b = &bucket{}
			groups[e.CreatorEmail] = b
		}
		b.bots++
		b.totalValue += e.TotalValue
		b.sumPct += e.PnLPercent
		b.trades += e.TradeCount
		if !b.seenAny || e.PnLPercent > b.bestPct {
			b.bestPct = e.PnLPercent
		}
		if !b.seenAny || e.PnLPercent < b.worstPct {
			b.worstPct = e.PnLPercent
		}
		b.seenAny = true
	}

	creators := make([]CreatorEntry, 0, len(groups))
	for email, b := range groups {
		creators = append(creators, CreatorEntry{
			CreatorEmail:    maskEmail(email),
			BotCount:        b.bots,
			TotalValue:      b.totalValue,
			AvgPnLPercent:   b.sumPct / float64(b.bots),
			BestBotPnLPct:   b.bestPct,
			WorstBotPnLPct:  b.worstPct,
			TotalTradeCount: b.trades,
		})
	}
	sort.SliceStable(creators, func(i, j int) bool {
		return creators[i].AvgPnLPercent > creators[j].AvgPnLPercent
	})
	for i := range creators {
		creators[i].Rank = i + 1
	}
	return creators
}

// maskEmail keeps the leaderboard public-friendly: alice@example.com -> a***e@example.com
func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at < 1 {
		return email
	}
	local := email[:at]
	domain := email[at:]
	if len(local) <= 2 {
		return local[:1] + "***" + domain
	}
	return string(local[0]) + "***" + string(local[len(local)-1]) + domain
}
