package services

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TradingEngine struct {
	marketService *MarketDataService
}

func NewTradingEngine() *TradingEngine {
	return &TradingEngine{
		marketService: GetMarketService(),
	}
}

// ExecuteStockTrade runs a stock buy or sell against the bot's main account.
func (te *TradingEngine) ExecuteStockTrade(bot models.Bot, req models.StockTradeRequest) (*models.Trade, error) {
	return te.executeStockTrade(bot, req, nil)
}

// ExecuteSeasonStockTrade routes a stock trade into the bot's isolated
// tournament account for the given active season. The bot must already be
// enrolled. Season trades read/write season_enrollments.cash_balance and
// positions/trades scoped to season_id.
func (te *TradingEngine) ExecuteSeasonStockTrade(bot models.Bot, seasonID uuid.UUID, req models.StockTradeRequest) (*models.Trade, error) {
	return te.executeStockTrade(bot, req, &seasonID)
}

func (te *TradingEngine) executeStockTrade(bot models.Bot, req models.StockTradeRequest, seasonID *uuid.UUID) (*models.Trade, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Side != "buy" && req.Side != "sell" {
		return nil, fmt.Errorf("side must be 'buy' or 'sell'")
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	quote, err := te.marketService.GetQuote(req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}
	var price float64
	if req.Side == "buy" {
		price = quote.Ask
	} else {
		price = quote.Bid
	}
	totalValue := price * float64(req.Quantity)

	tx, err := database.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Resolve the cash account for this trade. Season-scoped trades must
	// resolve an active enrollment with sufficient cash; main-account uses
	// the bot's persistent cash_balance.
	cashCtx, err := resolveCashContext(tx, bot.ID, seasonID)
	if err != nil {
		return nil, err
	}

	if req.Side == "buy" {
		if cashCtx.cashBalance < totalValue {
			return nil, fmt.Errorf("insufficient funds: need $%.2f, have $%.2f", totalValue, cashCtx.cashBalance)
		}
		if err := cashCtx.adjust(tx, -totalValue); err != nil {
			return nil, fmt.Errorf("failed to debit cash: %w", err)
		}
	} else {
		var currentQty int
		err := scopedQueryRow(tx, seasonID,
			`SELECT COALESCE(SUM(quantity), 0) FROM positions
			 WHERE bot_id = ?1 AND symbol = ?2 AND position_type = 'stock' AND `,
			"season_id IS NULL",
			"season_id = ?3",
			bot.ID.String(), req.Symbol,
		).Scan(&currentQty)
		if err != nil {
			return nil, fmt.Errorf("failed to check position: %w", err)
		}
		if currentQty < req.Quantity {
			return nil, fmt.Errorf("insufficient shares: need %d, have %d", req.Quantity, currentQty)
		}
		if err := cashCtx.adjust(tx, totalValue); err != nil {
			return nil, fmt.Errorf("failed to credit cash: %w", err)
		}
	}

	if err := te.updatePosition(tx, bot.ID, seasonID, req.Symbol, "stock", req.Quantity, price, req.Side); err != nil {
		return nil, fmt.Errorf("failed to update position: %w", err)
	}

	tradeID := uuid.New()
	var seasonIDStr sql.NullString
	if seasonID != nil {
		seasonIDStr = sql.NullString{String: seasonID.String(), Valid: true}
	}
	_, err = tx.Exec(
		`INSERT INTO trades (id, bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning, season_id)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
		tradeID.String(), bot.ID.String(), req.Symbol, "stock", req.Side, req.Quantity, price, totalValue, req.Reasoning, seasonIDStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trade: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	var trade models.Trade
	var tradeIDStr, botIDStr, executedAt string
	err = database.DB.QueryRow(
		`SELECT id, bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
		 FROM trades WHERE id = ?1`,
		tradeID.String(),
	).Scan(&tradeIDStr, &botIDStr, &trade.Symbol, &trade.TradeType, &trade.Side,
		&trade.Quantity, &trade.Price, &trade.TotalValue, &trade.Reasoning, &executedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trade: %w", err)
	}
	trade.ID, _ = uuid.Parse(tradeIDStr)
	trade.BotID, _ = uuid.Parse(botIDStr)
	trade.ExecutedAt, _ = time.Parse("2006-01-02 15:04:05", executedAt)
	return &trade, nil
}

// cashContext encapsulates "what cash am I trading against and how do I
// modify it." This is the core abstraction that lets the engine treat main
// and season accounts uniformly without per-call branching.
type cashContext struct {
	seasonID    *uuid.UUID
	botID       uuid.UUID
	cashBalance float64
}

func (c *cashContext) adjust(tx *sql.Tx, delta float64) error {
	if c.seasonID == nil {
		_, err := tx.Exec(
			`UPDATE bots SET cash_balance = cash_balance + ?1 WHERE id = ?2`,
			delta, c.botID.String(),
		)
		return err
	}
	_, err := tx.Exec(
		`UPDATE season_enrollments SET cash_balance = cash_balance + ?1
		 WHERE bot_id = ?2 AND season_id = ?3`,
		delta, c.botID.String(), c.seasonID.String(),
	)
	return err
}

func resolveCashContext(tx *sql.Tx, botID uuid.UUID, seasonID *uuid.UUID) (*cashContext, error) {
	ctx := &cashContext{seasonID: seasonID, botID: botID}
	if seasonID == nil {
		err := tx.QueryRow(
			`SELECT cash_balance FROM bots WHERE id = ?1`,
			botID.String(),
		).Scan(&ctx.cashBalance)
		if err != nil {
			return nil, fmt.Errorf("failed to load bot cash: %w", err)
		}
		return ctx, nil
	}

	// Season trade: refuse unless the season is active and the bot is
	// actually enrolled. Pending and closed seasons reject trades.
	var status string
	err := tx.QueryRow(
		`SELECT status FROM seasons WHERE id = ?1`,
		seasonID.String(),
	).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("season not found")
	}
	if err != nil {
		return nil, err
	}
	if status != models.SeasonStatusActive {
		return nil, fmt.Errorf("season is not active (status: %s)", status)
	}

	err = tx.QueryRow(
		`SELECT cash_balance FROM season_enrollments
		 WHERE bot_id = ?1 AND season_id = ?2`,
		botID.String(), seasonID.String(),
	).Scan(&ctx.cashBalance)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("bot is not enrolled in this season")
	}
	if err != nil {
		return nil, err
	}
	return ctx, nil
}

// scopedQueryRow lets a single query template work for both main and season
// scopes — the caller provides the SELECT prefix and the two predicate
// fragments, and we paste the right one in. Avoids duplicating every query.
func scopedQueryRow(tx *sql.Tx, seasonID *uuid.UUID, base, mainPred, seasonPred string, args ...interface{}) *sql.Row {
	if seasonID == nil {
		return tx.QueryRow(base+mainPred, args...)
	}
	args = append(args, seasonID.String())
	return tx.QueryRow(base+seasonPred, args...)
}

func (te *TradingEngine) updatePosition(tx *sql.Tx, botID uuid.UUID, seasonID *uuid.UUID, symbol, posType string, quantity int, price float64, side string) error {
	var existingIDStr string
	var existingQty int
	var existingAvgCost float64

	err := scopedQueryRow(tx, seasonID,
		`SELECT id, quantity, avg_cost FROM positions
		 WHERE bot_id = ?1 AND symbol = ?2 AND position_type = ?3 AND `,
		"season_id IS NULL",
		"season_id = ?4",
		botID.String(), symbol, posType,
	).Scan(&existingIDStr, &existingQty, &existingAvgCost)

	var existingID uuid.UUID
	if err == nil {
		existingID, _ = uuid.Parse(existingIDStr)
	}

	if err == sql.ErrNoRows {
		if side == "buy" {
			posID := uuid.New()
			var seasonIDStr sql.NullString
			if seasonID != nil {
				seasonIDStr = sql.NullString{String: seasonID.String(), Valid: true}
			}
			_, err = tx.Exec(
				`INSERT INTO positions (id, bot_id, symbol, position_type, quantity, avg_cost, season_id)
				 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
				posID.String(), botID.String(), symbol, posType, quantity, price, seasonIDStr)
			return err
		}
		return fmt.Errorf("no position to sell")
	} else if err != nil {
		return err
	}

	if side == "buy" {
		newQty := existingQty + quantity
		newAvgCost := ((existingAvgCost * float64(existingQty)) + (price * float64(quantity))) / float64(newQty)
		_, err = tx.Exec(
			`UPDATE positions SET quantity = ?1, avg_cost = ?2, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?3`,
			newQty, newAvgCost, existingID.String())
		return err
	}

	newQty := existingQty - quantity
	if newQty == 0 {
		_, err = tx.Exec(`DELETE FROM positions WHERE id = ?1`, existingID.String())
		return err
	}
	_, err = tx.Exec(
		`UPDATE positions SET quantity = ?1, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?2`,
		newQty, existingID.String())
	return err
}
