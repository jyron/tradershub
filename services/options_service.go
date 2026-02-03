package services

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/google/uuid"
)

type OptionsService struct {
	alpacaClient  *AlpacaClient
	tradingEngine *TradingEngine
}

func NewOptionsService() *OptionsService {
	return &OptionsService{
		alpacaClient:  GetAlpacaClient(),
		tradingEngine: NewTradingEngine(),
	}
}

func (os *OptionsService) GetOptionChain(symbol string) ([]models.OptionContract, error) {
	if os.alpacaClient == nil {
		return nil, fmt.Errorf("Alpaca client not initialized")
	}

	client := os.alpacaClient.GetTradingClient()

	// Filter by underlying symbol to avoid fetching all contracts
	req := alpaca.GetOptionContractsRequest{
		UnderlyingSymbols: symbol,
	}

	response, err := client.GetOptionContracts(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch option contracts: %w", err)
	}

	if response == nil || len(response) == 0 {
		return []models.OptionContract{}, nil
	}

	contracts := make([]models.OptionContract, 0, len(response))
	for _, contract := range response {

		optionType := "call"
		if contract.Type == "put" {
			optionType = "put"
		}

		openInterest := int64(0)
		if contract.OpenInterest != nil {
			openInterest = contract.OpenInterest.IntPart()
		}

		expDate := fmt.Sprintf("%04d-%02d-%02d", contract.ExpirationDate.Year, contract.ExpirationDate.Month, contract.ExpirationDate.Day)

		contracts = append(contracts, models.OptionContract{
			Symbol:           contract.Symbol,
			UnderlyingSymbol: contract.UnderlyingSymbol,
			OptionType:       optionType,
			StrikePrice:      contract.StrikePrice.InexactFloat64(),
			ExpirationDate:   expDate,
			Bid:              0,
			Ask:              0,
			LastPrice:        0,
			Volume:           0,
			OpenInterest:     openInterest,
		})
	}

	return contracts, nil
}

func (os *OptionsService) ExecuteOptionTrade(bot models.Bot, req models.OptionTradeRequest) (*models.Trade, error) {
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Side != "buy" && req.Side != "sell" {
		return nil, fmt.Errorf("side must be 'buy' or 'sell'")
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	if os.alpacaClient == nil {
		return nil, fmt.Errorf("Alpaca client not initialized")
	}

	client := os.alpacaClient.GetTradingClient()

	// Extract underlying symbol from option symbol (first 1-6 chars before numbers)
	underlying := extractUnderlyingSymbol(req.Symbol)

	// Filter by underlying symbol to avoid fetching all contracts
	contractReq := alpaca.GetOptionContractsRequest{
		UnderlyingSymbols: underlying,
	}
	contractResp, err := client.GetOptionContracts(contractReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch option contracts: %w", err)
	}

	var contract *alpaca.OptionContract
	for i := range contractResp {
		if contractResp[i].Symbol == req.Symbol {
			contract = &contractResp[i]
			break
		}
	}

	if contract == nil {
		return nil, fmt.Errorf("option contract not found: %s", req.Symbol)
	}

	// Get real market price for the option contract
	price, err := os.getOptionPrice(req.Symbol, req.Side)
	if err != nil {
		return nil, fmt.Errorf("failed to get option price: %w", err)
	}

	totalValue := price * float64(req.Quantity) * 100

	optionType := "call"
	if contract.Type == "put" {
		optionType = "put"
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	if req.Side == "buy" {
		if bot.CashBalance < totalValue {
			return nil, fmt.Errorf("insufficient funds: need $%.2f, have $%.2f", totalValue, bot.CashBalance)
		}
		_, err = tx.Exec(
			"UPDATE bots SET cash_balance = cash_balance - ? WHERE id = ?",
			totalValue, bot.ID.String())
	} else {
		var currentQty int
		err = tx.QueryRow(
			`SELECT COALESCE(SUM(quantity), 0) FROM positions
			 WHERE bot_id = ? AND symbol = ? AND position_type = ?`,
			bot.ID.String(), req.Symbol, optionType).Scan(&currentQty)
		if err != nil {
			return nil, fmt.Errorf("failed to check position: %w", err)
		}
		if currentQty < req.Quantity {
			return nil, fmt.Errorf("insufficient contracts: need %d, have %d", req.Quantity, currentQty)
		}

		_, err = tx.Exec(
			"UPDATE bots SET cash_balance = cash_balance + ? WHERE id = ?",
			totalValue, bot.ID.String())
	}

	if err != nil {
		return nil, fmt.Errorf("failed to update cash balance: %w", err)
	}

	strikePrice := contract.StrikePrice.InexactFloat64()
	expDate := time.Date(
		contract.ExpirationDate.Year,
		time.Month(contract.ExpirationDate.Month),
		contract.ExpirationDate.Day,
		0, 0, 0, 0, time.UTC,
	)

	if err := os.updateOptionPosition(tx, bot.ID, req.Symbol, optionType, req.Quantity, price, req.Side, strikePrice, expDate); err != nil {
		return nil, fmt.Errorf("failed to update position: %w", err)
	}

	tradeID := uuid.New()
	_, err = tx.Exec(
		`INSERT INTO trades (id, bot_id, symbol, trade_type, side, quantity, price, strike_price, expiration_date, total_value, reasoning)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tradeID, bot.ID, req.Symbol, optionType, req.Side, req.Quantity, price, strikePrice, expDate, totalValue, req.Reasoning,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create trade: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	var trade models.Trade
	err = database.DB.QueryRow(
		`SELECT id, bot_id, symbol, trade_type, side, quantity, price, strike_price, expiration_date, total_value, reasoning, executed_at
		 FROM trades WHERE id = ?`,
		tradeID,
	).Scan(&trade.ID, &trade.BotID, &trade.Symbol, &trade.TradeType, &trade.Side,
		&trade.Quantity, &trade.Price, &trade.StrikePrice, &trade.ExpirationDate, &trade.TotalValue, &trade.Reasoning, &trade.ExecutedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch trade: %w", err)
	}

	return &trade, nil
}

func (os *OptionsService) updateOptionPosition(tx *sql.Tx, botID uuid.UUID, symbol string, optionType string, quantity int, price float64, side string, strikePrice float64, expirationDate time.Time) error {
	var existingID uuid.UUID
	var existingQty int
	var existingAvgCost float64
	var existingStrike float64
	var existingExpiration time.Time

	err := tx.QueryRow(
		`SELECT id, quantity, avg_cost, strike_price, expiration_date FROM positions
		 WHERE bot_id = ? AND symbol = ? AND position_type = ?`,
		botID, symbol, optionType,
	).Scan(&existingID, &existingQty, &existingAvgCost, &existingStrike, &existingExpiration)

	if err == sql.ErrNoRows {
		if side == "buy" {
			_, err = tx.Exec(
				`INSERT INTO positions (bot_id, symbol, position_type, quantity, avg_cost, strike_price, expiration_date)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				botID, symbol, optionType, quantity, price, strikePrice, expirationDate)
			return err
		} else {
			return fmt.Errorf("no position to sell")
		}
	} else if err != nil {
		return err
	}

	if side == "buy" {
		newQty := existingQty + quantity
		newAvgCost := ((existingAvgCost * float64(existingQty)) + (price * float64(quantity))) / float64(newQty)

		_, err = tx.Exec(
			`UPDATE positions SET quantity = ?, avg_cost = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			newQty, newAvgCost, existingID)
		return err
	} else {
		newQty := existingQty - quantity

		if newQty == 0 {
			_, err = tx.Exec(
				"DELETE FROM positions WHERE id = ?", existingID)
			return err
		} else {
			_, err = tx.Exec(
				`UPDATE positions SET quantity = ?, updated_at = CURRENT_TIMESTAMP
				 WHERE id = ?`,
				newQty, existingID)
			return err
		}
	}
}

func (os *OptionsService) ParseOptionSymbol(symbol string) (underlying string, optionType string, strikePrice float64, expirationDate string, err error) {
	parts := strings.Split(symbol, " ")
	if len(parts) < 2 {
		return "", "", 0, "", fmt.Errorf("invalid option symbol format")
	}

	underlying = parts[0]

	return underlying, "", 0, "", nil
}

// extractUnderlyingSymbol extracts the underlying stock symbol from an option contract symbol
// Example: "AAPL260202C00175000" -> "AAPL"
func extractUnderlyingSymbol(optionSymbol string) string {
	// Option symbols are: SYMBOL + YYMMDD + C/P + STRIKE
	// Find the first digit to determine where the symbol ends
	for i, char := range optionSymbol {
		if char >= '0' && char <= '9' {
			return optionSymbol[:i]
		}
	}
	return optionSymbol // Fallback if no digits found
}

// GetCurrentOptionPrice fetches the current market price for an option contract
// Uses the midpoint of bid/ask for valuation purposes
func (os *OptionsService) GetCurrentOptionPrice(contractSymbol string) (float64, error) {
	if os.alpacaClient == nil {
		return 0, fmt.Errorf("Alpaca client not initialized")
	}

	marketClient := os.alpacaClient.GetMarketDataClient()

	quotes, err := marketClient.GetLatestOptionQuotes([]string{contractSymbol}, marketdata.GetLatestOptionQuoteRequest{})
	if err != nil {
		return 0, fmt.Errorf("failed to fetch option quote: %w", err)
	}

	quote, exists := quotes[contractSymbol]
	if !exists {
		return 0, fmt.Errorf("no quote available for contract %s", contractSymbol)
	}

	// Use midpoint for fair value, fallback to bid or ask if one is missing
	if quote.BidPrice > 0 && quote.AskPrice > 0 {
		return (quote.BidPrice + quote.AskPrice) / 2, nil
	} else if quote.BidPrice > 0 {
		return quote.BidPrice, nil
	} else if quote.AskPrice > 0 {
		return quote.AskPrice, nil
	}

	return 0, fmt.Errorf("no valid bid/ask price for contract %s", contractSymbol)
}

// getOptionPrice fetches the real market price for an option contract from Alpaca
func (os *OptionsService) getOptionPrice(contractSymbol string, side string) (float64, error) {
	if os.alpacaClient == nil {
		return 0, fmt.Errorf("Alpaca client not initialized")
	}

	marketClient := os.alpacaClient.GetMarketDataClient()

	// Get latest quote (bid/ask) for the option contract
	quotes, err := marketClient.GetLatestOptionQuotes([]string{contractSymbol}, marketdata.GetLatestOptionQuoteRequest{})
	if err != nil {
		return 0, fmt.Errorf("failed to fetch option quote: %w", err)
	}

	quote, exists := quotes[contractSymbol]
	if !exists {
		return 0, fmt.Errorf("no quote available for contract %s", contractSymbol)
	}

	// Use ask price for buys (what you pay), bid price for sells (what you receive)
	var price float64
	if side == "buy" {
		if quote.AskPrice <= 0 {
			return 0, fmt.Errorf("invalid ask price for contract %s: %.2f", contractSymbol, quote.AskPrice)
		}
		price = quote.AskPrice
	} else {
		if quote.BidPrice <= 0 {
			return 0, fmt.Errorf("invalid bid price for contract %s: %.2f", contractSymbol, quote.BidPrice)
		}
		price = quote.BidPrice
	}

	return price, nil
}
