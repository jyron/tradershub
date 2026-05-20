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

// loadEntries used to recompute each bot's portfolio live by calling
// GetPortfolio() in an N+1 loop — that meant 40+ Finnhub round-trips per
// leaderboard render and ~5.6s response times. We now read the latest
// portfolio_snapshot per bot in one SQL query and trust the hourly
// PortfolioSnapshotJob to keep snapshots fresh. Per-bot detail pages still
// recompute live for accuracy.

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
	ModelProvider string   `json:"model_provider,omitempty"`
	IsOfficial    bool     `json:"is_official,omitempty"`
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
			COALESCE(b.model_provider, ''),
			COALESCE(b.is_official, 0),
			COALESCE(b.cash_balance, ?1) AS cash_balance,
			COALESCE(COUNT(DISTINCT t.id), 0) AS trade_count,
			(SELECT total_value FROM portfolio_snapshots
			 WHERE bot_id = b.id AND season_id IS NULL
			 ORDER BY snapshot_at DESC LIMIT 1) AS latest_total_value
		FROM bots b
		LEFT JOIN trades t ON b.id = t.bot_id AND t.season_id IS NULL
		WHERE b.is_active = 1 AND COALESCE(b.is_official, 0) = 1
		GROUP BY b.id, b.name, b.creator_email, b.model_provider, b.is_official, b.cash_balance
	`, startingBalance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rowsOut []entryRow
	for rows.Next() {
		var idStr, name, email, modelProvider string
		var isOfficial, tradeCount int
		var cashBalance float64
		var latestTotal *float64
		if err := rows.Scan(&idStr, &name, &email, &modelProvider, &isOfficial, &cashBalance, &tradeCount, &latestTotal); err != nil {
			continue
		}
		botID, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}

		// Snapshot is the source of truth; fall back to current cash for
		// freshly-registered bots that haven't had a snapshot written yet.
		totalValue := cashBalance
		if latestTotal != nil {
			totalValue = *latestTotal
		}
		pnl := totalValue - startingBalance
		pnlPct := 0.0
		if startingBalance > 0 {
			pnlPct = (pnl / startingBalance) * 100
		}

		rowsOut = append(rowsOut, entryRow{
			entry: LeaderboardEntry{
				BotID:         botID.String(),
				BotName:       name,
				CreatorEmail:  email,
				ModelProvider: modelProvider,
				IsOfficial:    isOfficial != 0,
				TotalValue:    totalValue,
				PnL:           pnl,
				PnLPercent:    pnlPct,
				TradeCount:    tradeCount,
			},
			botID: botID,
		})
	}

	// Batch-fetch all snapshot value series in one query rather than N round
	// trips to Turso. Group in Go by bot_id, preserving snapshot order.
	history := loadSnapshotHistoryByBot(rowsOut)

	entries := make([]LeaderboardEntry, 0, len(rowsOut))
	for _, r := range rowsOut {
		e := r.entry
		values := history[r.botID.String()]
		e.SnapshotCount = len(values)
		metrics := services.ComputeMetrics(values)
		e.Eligible = e.TradeCount >= MinTradesForRanking && metrics.Valid
		if e.Eligible {
			s, so, dd := metrics.Sharpe, metrics.Sortino, metrics.MaxDrawdown
			e.Sharpe = &s
			e.Sortino = &so
			e.MaxDrawdown = &dd
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// loadSnapshotHistoryByBot pulls every (bot_id, total_value) pair for the
// given bots in one query and groups in memory. Ordering by bot_id then
// snapshot_at means values come out in chronological order per bot, which is
// what ComputeMetrics expects.
type entryRow struct {
	entry LeaderboardEntry
	botID uuid.UUID
}

func loadSnapshotHistoryByBot(rows []entryRow) map[string][]float64 {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]interface{}, len(rows))
	placeholders := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.botID.String()
		placeholders[i] = "?"
	}
	q := fmt.Sprintf(
		`SELECT bot_id, total_value FROM portfolio_snapshots
		 WHERE season_id IS NULL AND bot_id IN (%s)
		 ORDER BY bot_id, snapshot_at ASC`,
		strings.Join(placeholders, ","),
	)
	out := make(map[string][]float64, len(rows))
	dbRows, err := database.DB.Query(q, ids...)
	if err != nil {
		return out
	}
	defer dbRows.Close()
	for dbRows.Next() {
		var botID string
		var v float64
		if err := dbRows.Scan(&botID, &v); err == nil {
			out[botID] = append(out[botID], v)
		}
	}
	return out
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
