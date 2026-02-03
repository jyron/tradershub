package services

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"fmt"

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

func (te *TradingEngine) ExecuteStockTrade(bot models.Bot, req models.StockTradeRequest) (*models.Trade, error) {
	// Validate input
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Side != "buy" && req.Side != "sell" {
		return nil, fmt.Errorf("side must be 'buy' or 'sell'")
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	// Get current market price
	quote, err := te.marketService.GetQuote(req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}

	// Use ask price for buys, bid price for sells
	var price float64
	if req.Side == "buy" {
		price = quote.Ask
	} else {
		price = quote.Bid
	}

	totalValue := price * float64(req.Quantity)

	// Start a transaction
	tx, err := database.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Validate and update cash balance
	if req.Side == "buy" {
		if bot.CashBalance < totalValue {
			return nil, fmt.Errorf("insufficient funds: need $%.2f, have $%.2f", totalValue, bot.CashBalance)
		}
		_, err = tx.Exec(
			"UPDATE bots SET cash_balance = cash_balance - ? WHERE id = ?",
			totalValue, bot.ID.String())
	} else {
		// For sells, verify we have the position
		var currentQty int
		err = tx.QueryRow(
			`SELECT COALESCE(SUM(quantity), 0) FROM positions
			 WHERE bot_id = ? AND symbol = ? AND position_type = 'stock'`,
			bot.ID.String(), req.Symbol).Scan(&currentQty)
		if err != nil {
			return nil, fmt.Errorf("failed to check position: %w", err)
		}
		if currentQty < req.Quantity {
			return nil, fmt.Errorf("insufficient shares: need %d, have %d", req.Quantity, currentQty)
		}

		_, err = tx.Exec(
			"UPDATE bots SET cash_balance = cash_balance + ? WHERE id = ?",
			totalValue, bot.ID.String())
	}

	if err != nil {
		return nil, fmt.Errorf("failed to update cash balance: %w", err)
	}

	// Update or create position
	if err := te.updatePosition(tx, bot.ID, req.Symbol, "stock", req.Quantity, price, req.Side); err != nil {
		return nil, fmt.Errorf("failed to update position: %w", err)
	}

	// Create trade record
	tradeID := uuid.New()
	_, err = tx.Exec(
		`INSERT INTO trades (id, bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tradeID, bot.ID, req.Symbol, "stock", req.Side, req.Quantity, price, totalValue, req.Reasoning,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trade: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Return trade details
	var trade models.Trade
	err = database.DB.QueryRow(
		`SELECT id, bot_id, symbol, trade_type, side, quantity, price, total_value, reasoning, executed_at
		 FROM trades WHERE id = ?`,
		tradeID,
	).Scan(&trade.ID, &trade.BotID, &trade.Symbol, &trade.TradeType, &trade.Side,
		&trade.Quantity, &trade.Price, &trade.TotalValue, &trade.Reasoning, &trade.ExecutedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch trade: %w", err)
	}

	return &trade, nil
}

func (te *TradingEngine) updatePosition(tx *sql.Tx, botID uuid.UUID, symbol string, posType string, quantity int, price float64, side string) error {
	// Check if position exists
	var existingID uuid.UUID
	var existingQty int
	var existingAvgCost float64

	err := tx.QueryRow(
		`SELECT id, quantity, avg_cost FROM positions
		 WHERE bot_id = ? AND symbol = ? AND position_type = ?`,
		botID, symbol, posType,
	).Scan(&existingID, &existingQty, &existingAvgCost)

	if err == sql.ErrNoRows {
		// No existing position
		if side == "buy" {
			// Create new position
			_, err = tx.Exec(
				`INSERT INTO positions (bot_id, symbol, position_type, quantity, avg_cost)
				 VALUES (?, ?, ?, ?, ?)`,
				botID, symbol, posType, quantity, price)
			return err
		} else {
			// Can't sell what we don't have
			return fmt.Errorf("no position to sell")
		}
	} else if err != nil {
		return err
	}

	// Position exists, update it
	if side == "buy" {
		// Add to position, update average cost
		newQty := existingQty + quantity
		newAvgCost := ((existingAvgCost * float64(existingQty)) + (price * float64(quantity))) / float64(newQty)

		_, err = tx.Exec(
			`UPDATE positions SET quantity = ?, avg_cost = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			newQty, newAvgCost, existingID)
		return err
	} else {
		// Reduce position
		newQty := existingQty - quantity

		if newQty == 0 {
			// Close position
			_, err = tx.Exec(
				"DELETE FROM positions WHERE id = ?", existingID)
			return err
		} else {
			// Update quantity (keep same avg cost)
			_, err = tx.Exec(
				`UPDATE positions SET quantity = ?, updated_at = CURRENT_TIMESTAMP
				 WHERE id = ?`,
				newQty, existingID)
			return err
		}
	}
}
