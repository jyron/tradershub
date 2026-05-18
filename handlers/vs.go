package handlers

import (
	"bottrade/database"
	"bottrade/models"
	"bottrade/services"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Vs is the 1v1 head-to-head comparison endpoint.
//
//	GET /api/vs?a=<bot_id>&b=<bot_id>[&season_id=<id>][&trades=<n>]
//
// Returns side-by-side bot data plus a head_to_head block (leader, value diff,
// shared symbols). When season_id is provided, both bots are compared on that
// season's isolated tournament account; otherwise main accounts are compared.
//
// Visual rendering is intentionally minimal in static/vs.html — the response
// shape is what matters here so the eventual visual pass has stable data.
func Vs(c *fiber.Ctx) error {
	aStr := c.Query("a")
	bStr := c.Query("b")
	if aStr == "" || bStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "both 'a' and 'b' query parameters are required",
		})
	}
	if aStr == bStr {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "a and b must be different bots",
		})
	}
	aID, err := uuid.Parse(aStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid bot id 'a'"})
	}
	bID, err := uuid.Parse(bStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid bot id 'b'"})
	}

	var seasonID *uuid.UUID
	if s := c.Query("season_id"); s != "" {
		sid, err := uuid.Parse(s)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid season_id"})
		}
		seasonID = &sid
	}

	tradesLimit := 10
	if t := c.QueryInt("trades", 0); t > 0 {
		tradesLimit = t
	}
	if tradesLimit > 50 {
		tradesLimit = 50
	}

	sideA, err := buildVsSide(aID, seasonID, tradesLimit)
	if err != nil {
		return vsErr(c, "a", seasonID != nil, err)
	}
	sideB, err := buildVsSide(bID, seasonID, tradesLimit)
	if err != nil {
		return vsErr(c, "b", seasonID != nil, err)
	}

	resp := fiber.Map{
		"a":           sideA,
		"b":           sideB,
		"head_to_head": headToHead(sideA, sideB),
	}
	if seasonID != nil {
		resp["season_id"] = seasonID.String()
	}
	return c.JSON(resp)
}

func vsErr(c *fiber.Ctx, side string, seasonScoped bool, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		msg := fmt.Sprintf("bot '%s' not found", side)
		if seasonScoped {
			msg = fmt.Sprintf("bot '%s' not found or not enrolled in this season", side)
		}
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": msg})
	}
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": fmt.Sprintf("failed to load side '%s': %s", side, err.Error()),
	})
}

type vsSide struct {
	BotID            uuid.UUID                  `json:"bot_id"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description"`
	CreatorEmail     string                     `json:"creator_email"`
	Claimed          bool                       `json:"claimed"`
	CreatedAt        time.Time                  `json:"created_at"`
	CashBalance      float64                    `json:"cash_balance"`
	PositionsValue   float64                    `json:"positions_value"`
	TotalValue       float64                    `json:"total_value"`
	TotalPnL         float64                    `json:"total_pnl"`
	TotalPnLPct      float64                    `json:"total_pnl_percent"`
	PositionsCount   int                        `json:"positions_count"`
	Symbols          []string                   `json:"symbols"`
	Positions        []models.PositionWithValue `json:"positions"`
	TradeCount       int                        `json:"trade_count"`
	RecentTrades     []vsTrade                  `json:"recent_trades"`
	Snapshots        []vsSnapshot               `json:"portfolio_snapshots"`
}

type vsTrade struct {
	ID         string    `json:"id"`
	Symbol     string    `json:"symbol"`
	TradeType  string    `json:"trade_type"`
	Side       string    `json:"side"`
	Quantity   int       `json:"quantity"`
	Price      float64   `json:"price"`
	TotalValue float64   `json:"total_value"`
	Reasoning  string    `json:"reasoning"`
	ExecutedAt time.Time `json:"executed_at"`
}

type vsSnapshot struct {
	SnapshotAt time.Time `json:"snapshot_at"`
	TotalValue float64   `json:"total_value"`
}

func buildVsSide(botID uuid.UUID, seasonID *uuid.UUID, tradesLimit int) (*vsSide, error) {
	// 1. Bot identity. description/creator_email are nullable in the schema,
	// so we scan into NullString to stay portable across drivers (Turso is
	// lenient about NULL → "", modernc.org/sqlite is strict).
	var (
		dbID, name, createdAt string
		desc, creator         sql.NullString
		claimedInt            int
	)
	err := database.DB.QueryRow(
		`SELECT id, name, description, creator_email, claimed, created_at
		 FROM bots WHERE id = ?1`,
		botID.String(),
	).Scan(&dbID, &name, &desc, &creator, &claimedInt, &createdAt)
	if err != nil {
		return nil, err
	}
	parsedCreatedAt, perr := time.Parse("2006-01-02 15:04:05", createdAt)
	if perr != nil {
		parsedCreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	// 2. Portfolio (scope-aware)
	ps := services.NewPortfolioService()
	var portfolio *services.Portfolio
	if seasonID == nil {
		portfolio, err = ps.GetPortfolio(botID)
	} else {
		portfolio, err = ps.GetSeasonPortfolio(botID, *seasonID)
	}
	if err != nil {
		return nil, err
	}

	positionsValue := portfolio.TotalValue - portfolio.CashBalance
	symbols := make([]string, 0, len(portfolio.Positions))
	for _, p := range portfolio.Positions {
		symbols = append(symbols, p.Symbol)
	}

	// 3. Recent trades + total trade count (scoped to main vs season)
	trades, tradeCount, err := vsTradesAndCount(botID, seasonID, tradesLimit)
	if err != nil {
		return nil, err
	}

	// 4. Portfolio snapshots for the equity-curve overlay
	snapshots, err := vsSnapshots(botID, seasonID)
	if err != nil {
		return nil, err
	}

	return &vsSide{
		BotID:          botID,
		Name:           name,
		Description:    desc.String,
		CreatorEmail:   creator.String,
		Claimed:        claimedInt != 0,
		CreatedAt:      parsedCreatedAt,
		CashBalance:    portfolio.CashBalance,
		PositionsValue: positionsValue,
		TotalValue:     portfolio.TotalValue,
		TotalPnL:       portfolio.TotalPnL,
		TotalPnLPct:    portfolio.TotalPnLPct,
		PositionsCount: len(portfolio.Positions),
		Symbols:        symbols,
		Positions:      portfolio.Positions,
		TradeCount:     tradeCount,
		RecentTrades:   trades,
		Snapshots:      snapshots,
	}, nil
}

func vsTradesAndCount(botID uuid.UUID, seasonID *uuid.UUID, limit int) ([]vsTrade, int, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if seasonID == nil {
		rows, err = database.DB.Query(
			`SELECT id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
			 FROM trades
			 WHERE bot_id = ?1 AND season_id IS NULL
			 ORDER BY executed_at DESC
			 LIMIT ?2`,
			botID.String(), limit,
		)
	} else {
		rows, err = database.DB.Query(
			`SELECT id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
			 FROM trades
			 WHERE bot_id = ?1 AND season_id = ?2
			 ORDER BY executed_at DESC
			 LIMIT ?3`,
			botID.String(), seasonID.String(), limit,
		)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	trades := []vsTrade{}
	for rows.Next() {
		var t vsTrade
		var executedAtStr sql.NullString
		var reasoning sql.NullString
		if err := rows.Scan(
			&t.ID, &t.Symbol, &t.TradeType, &t.Side,
			&t.Quantity, &t.Price, &t.TotalValue, &reasoning, &executedAtStr,
		); err != nil {
			return nil, 0, err
		}
		if reasoning.Valid {
			t.Reasoning = reasoning.String
		}
		t.ExecutedAt = parseFlexTime(executedAtStr)
		trades = append(trades, t)
	}

	var total int
	if seasonID == nil {
		_ = database.DB.QueryRow(
			`SELECT COUNT(*) FROM trades WHERE bot_id = ?1 AND season_id IS NULL`,
			botID.String(),
		).Scan(&total)
	} else {
		_ = database.DB.QueryRow(
			`SELECT COUNT(*) FROM trades WHERE bot_id = ?1 AND season_id = ?2`,
			botID.String(), seasonID.String(),
		).Scan(&total)
	}
	return trades, total, nil
}

func vsSnapshots(botID uuid.UUID, seasonID *uuid.UUID) ([]vsSnapshot, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if seasonID == nil {
		rows, err = database.DB.Query(
			`SELECT snapshot_at, total_value FROM portfolio_snapshots
			 WHERE bot_id = ?1 AND season_id IS NULL
			 ORDER BY snapshot_at ASC`,
			botID.String(),
		)
	} else {
		rows, err = database.DB.Query(
			`SELECT snapshot_at, total_value FROM portfolio_snapshots
			 WHERE bot_id = ?1 AND season_id = ?2
			 ORDER BY snapshot_at ASC`,
			botID.String(), seasonID.String(),
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vsSnapshot{}
	for rows.Next() {
		var s vsSnapshot
		var ts sql.NullString
		if err := rows.Scan(&ts, &s.TotalValue); err != nil {
			return nil, err
		}
		s.SnapshotAt = parseFlexTime(ts)
		out = append(out, s)
	}
	return out, nil
}

// parseFlexTime accepts RFC3339 or "2006-01-02 15:04:05" (SQLite default).
func parseFlexTime(s sql.NullString) time.Time {
	if !s.Valid {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return t
		}
	}
	return time.Time{}
}

func headToHead(a, b *vsSide) fiber.Map {
	// Shared symbols (positions held by both). Small expected size, O(n*m)
	// is fine.
	shared := []string{}
	for _, sa := range a.Symbols {
		for _, sb := range b.Symbols {
			if sa == sb {
				shared = append(shared, sa)
				break
			}
		}
	}

	var leader string
	if a.TotalValue > b.TotalValue {
		leader = a.BotID.String()
	} else if b.TotalValue > a.TotalValue {
		leader = b.BotID.String()
	} else {
		leader = ""
	}

	valueDiff := a.TotalValue - b.TotalValue
	var valueDiffPct float64
	if b.TotalValue > 0 {
		valueDiffPct = (valueDiff / b.TotalValue) * 100
	}
	returnGap := a.TotalPnLPct - b.TotalPnLPct
	tradeGap := a.TradeCount - b.TradeCount

	return fiber.Map{
		"leader_bot_id":      leader,
		"value_diff_a_minus_b": valueDiff,
		"value_diff_pct":     valueDiffPct,
		"return_gap_pct":     returnGap,
		"trade_count_diff":   tradeGap,
		"shared_symbols":     shared,
		"a_symbols_only":     diffSymbols(a.Symbols, b.Symbols),
		"b_symbols_only":     diffSymbols(b.Symbols, a.Symbols),
	}
}

// diffSymbols returns elements of `have` not present in `other`.
func diffSymbols(have, other []string) []string {
	out := []string{}
	for _, s := range have {
		found := false
		for _, o := range other {
			if s == o {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s)
		}
	}
	return out
}
