package services

import (
	"bottrade/database"
	"bottrade/models"
	"context"

	"github.com/google/uuid"
)

type PortfolioService struct {
	marketService *MarketDataService
}

func NewPortfolioService() *PortfolioService {
	return &PortfolioService{
		marketService: GetMarketService(),
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
	ctx := context.Background()

	// Get bot info
	var bot models.Bot
	err := database.DB.QueryRow(ctx,
		`SELECT id, name, cash_balance FROM bots WHERE id = $1`,
		botID,
	).Scan(&bot.ID, &bot.Name, &bot.CashBalance)

	if err != nil {
		return nil, err
	}

	// Get all positions
	rows, err := database.DB.Query(ctx,
		`SELECT id, bot_id, symbol, position_type, quantity, avg_cost, strike_price, expiration_date, created_at, updated_at
		 FROM positions WHERE bot_id = $1`,
		botID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []models.PositionWithValue
	totalPositionValue := 0.0

	for rows.Next() {
		var pos models.Position
		err := rows.Scan(
			&pos.ID, &pos.BotID, &pos.Symbol, &pos.PositionType,
			&pos.Quantity, &pos.AvgCost, &pos.StrikePrice, &pos.ExpirationDate,
			&pos.CreatedAt, &pos.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Get current price
		quote, err := ps.marketService.GetQuote(pos.Symbol)
		if err != nil {
			// If we can't get a quote, skip this position
			continue
		}

		marketValue := quote.Price * float64(pos.Quantity)
		costBasis := pos.AvgCost * float64(pos.Quantity)
		unrealizedPnL := marketValue - costBasis

		posWithValue := models.PositionWithValue{
			Position:      pos,
			CurrentPrice:  quote.Price,
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
