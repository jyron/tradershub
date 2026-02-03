package services

import (
	"bottrade/database"
	"bottrade/models"

	"github.com/google/uuid"
)

type PortfolioService struct {
	marketService  *MarketDataService
	optionsService *OptionsService
}

func NewPortfolioService() *PortfolioService {
	return &PortfolioService{
		marketService:  GetMarketService(),
		optionsService: NewOptionsService(),
	}
}

type Portfolio struct {
	BotID         uuid.UUID                    `json:"bot_id"`
	BotName       string                       `json:"bot_name"`
	CashBalance   float64                      `json:"cash_balance"`
	Positions     []models.PositionWithValue   `json:"positions"`
	TotalValue    float64                      `json:"total_value"`
	TotalPnL      float64                      `json:"total_pnl"`
	TotalPnLPct   float64                      `json:"total_pnl_percent"`
}

func (ps *PortfolioService) GetPortfolio(botID uuid.UUID) (*Portfolio, error) {

	// Get bot info
	var bot models.Bot
	var botIDStr string
	err := database.DB.QueryRow(		`SELECT id, name, cash_balance FROM bots WHERE id = ?`,
		botID.String(),
	).Scan(&botIDStr, &bot.Name, &bot.CashBalance)

	if err != nil {
		return nil, err
	}

	// Parse the ID string back to UUID
	bot.ID, err = uuid.Parse(botIDStr)
	if err != nil {
		return nil, err
	}

	// Get all positions
	rows, err := database.DB.Query(		`SELECT id, bot_id, symbol, position_type, quantity, avg_cost, strike_price, expiration_date, created_at, updated_at
		 FROM positions WHERE bot_id = ?`,
		botID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []models.PositionWithValue
	totalPositionValue := 0.0

	for rows.Next() {
		var pos models.Position
		var posIDStr, posBotIDStr string
		err := rows.Scan(
			&posIDStr, &posBotIDStr, &pos.Symbol, &pos.PositionType,
			&pos.Quantity, &pos.AvgCost, &pos.StrikePrice, &pos.ExpirationDate,
			&pos.CreatedAt, &pos.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Parse UUID strings
		pos.ID, err = uuid.Parse(posIDStr)
		if err != nil {
			return nil, err
		}
		pos.BotID, err = uuid.Parse(posBotIDStr)
		if err != nil {
			return nil, err
		}

		var currentPrice float64
		var marketValue float64
		var costBasis float64

		// Determine if this is a stock or option position
		if pos.PositionType == "stock" {
			// Stock position - get quote from market data service
			quote, err := ps.marketService.GetQuote(pos.Symbol)
			if err != nil {
				// Skip position if we can't get a price
				continue
			}
			currentPrice = quote.Price
			marketValue = currentPrice * float64(pos.Quantity)
			costBasis = pos.AvgCost * float64(pos.Quantity)
		} else if pos.PositionType == "call" || pos.PositionType == "put" {
			// Option position - get quote from options service
			price, err := ps.optionsService.GetCurrentOptionPrice(pos.Symbol)
			if err != nil {
				// Skip position if we can't get a price
				continue
			}
			currentPrice = price
			// Options: price is per share, but each contract = 100 shares
			marketValue = currentPrice * float64(pos.Quantity) * 100
			costBasis = pos.AvgCost * float64(pos.Quantity) * 100
		} else {
			// Unknown position type, skip
			continue
		}

		unrealizedPnL := marketValue - costBasis

		posWithValue := models.PositionWithValue{
			Position:      pos,
			CurrentPrice:  currentPrice,
			MarketValue:   marketValue,
			UnrealizedPnL: unrealizedPnL,
		}

		positions = append(positions, posWithValue)
		totalPositionValue += marketValue
	}

	totalValue := bot.CashBalance + totalPositionValue
	startingBalance := 100000.0 // Default starting balance
	totalPnL := totalValue - startingBalance
	totalPnLPct := (totalPnL / startingBalance) * 100

	return &Portfolio{
		BotID:       bot.ID,
		BotName:     bot.Name,
		CashBalance: bot.CashBalance,
		Positions:   positions,
		TotalValue:  totalValue,
		TotalPnL:    totalPnL,
		TotalPnLPct: totalPnLPct,
	}, nil
}
