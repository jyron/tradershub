package services

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"time"

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
	BotID       uuid.UUID                  `json:"bot_id"`
	BotName     string                     `json:"bot_name"`
	SeasonID    *uuid.UUID                 `json:"season_id,omitempty"`
	CashBalance float64                    `json:"cash_balance"`
	Positions   []models.PositionWithValue `json:"positions"`
	TotalValue  float64                    `json:"total_value"`
	TotalPnL    float64                    `json:"total_pnl"`
	TotalPnLPct float64                    `json:"total_pnl_percent"`
}

// GetPortfolio returns the bot's main-account portfolio. Season-scoped
// portfolios go through GetSeasonPortfolio so the call sites are explicit.
func (ps *PortfolioService) GetPortfolio(botID uuid.UUID) (*Portfolio, error) {
	return ps.getPortfolio(botID, nil)
}

// GetSeasonPortfolio returns the bot's isolated tournament portfolio for the
// named season — cash from season_enrollments.cash_balance plus
// mark-to-market position value for positions with season_id = seasonID.
func (ps *PortfolioService) GetSeasonPortfolio(botID, seasonID uuid.UUID) (*Portfolio, error) {
	return ps.getPortfolio(botID, &seasonID)
}

func (ps *PortfolioService) getPortfolio(botID uuid.UUID, seasonID *uuid.UUID) (*Portfolio, error) {
	var bot models.Bot
	var botIDStr string
	err := database.DB.QueryRow(
		`SELECT id, name FROM bots WHERE id = ?1`,
		botID.String(),
	).Scan(&botIDStr, &bot.Name)
	if err != nil {
		return nil, err
	}
	bot.ID, err = uuid.Parse(botIDStr)
	if err != nil {
		return nil, err
	}

	// Cash source differs by scope: main account reads bots.cash_balance,
	// season account reads season_enrollments.cash_balance.
	var cashBalance float64
	var startingBalance float64 = 100000.0
	if seasonID == nil {
		err = database.DB.QueryRow(
			`SELECT cash_balance FROM bots WHERE id = ?1`,
			botID.String(),
		).Scan(&cashBalance)
	} else {
		err = database.DB.QueryRow(
			`SELECT e.cash_balance, s.starting_balance
			 FROM season_enrollments e
			 JOIN seasons s ON s.id = e.season_id
			 WHERE e.bot_id = ?1 AND e.season_id = ?2`,
			botID.String(), seasonID.String(),
		).Scan(&cashBalance, &startingBalance)
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
	}
	if err != nil {
		return nil, err
	}

	// Positions in the requested scope. NULL season_id = main account.
	var posRows *sql.Rows
	if seasonID == nil {
		posRows, err = database.DB.Query(
			`SELECT id, bot_id, symbol, position_type, quantity, avg_cost, strike_price, expiration_date, created_at, updated_at
			 FROM positions
			 WHERE bot_id = ?1 AND season_id IS NULL`,
			botID.String(),
		)
	} else {
		posRows, err = database.DB.Query(
			`SELECT id, bot_id, symbol, position_type, quantity, avg_cost, strike_price, expiration_date, created_at, updated_at
			 FROM positions
			 WHERE bot_id = ?1 AND season_id = ?2`,
			botID.String(), seasonID.String(),
		)
	}
	if err != nil {
		return nil, err
	}
	defer posRows.Close()

	// Scan time-typed columns as strings; the pure-Go sqlite driver doesn't
	// auto-convert TEXT timestamps to time.Time. Parse them after the scan.
	type rawPos struct{ models.Position }
	var raws []rawPos
	for posRows.Next() {
		var pos models.Position
		var posIDStr, posBotIDStr string
		var expirationStr sql.NullString
		var createdAt, updatedAt sql.NullString
		if err := posRows.Scan(
			&posIDStr, &posBotIDStr, &pos.Symbol, &pos.PositionType,
			&pos.Quantity, &pos.AvgCost, &pos.StrikePrice, &expirationStr,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		if pos.ID, err = uuid.Parse(posIDStr); err != nil {
			return nil, err
		}
		if pos.BotID, err = uuid.Parse(posBotIDStr); err != nil {
			return nil, err
		}
		if expirationStr.Valid {
			if t, err := time.Parse("2006-01-02", expirationStr.String); err == nil {
				pos.ExpirationDate = &t
			}
		}
		pos.CreatedAt = parsePositionTime(createdAt)
		pos.UpdatedAt = parsePositionTime(updatedAt)
		raws = append(raws, rawPos{pos})
	}
	posRows.Close()

	var positions []models.PositionWithValue
	totalPositionValue := 0.0
	for _, rp := range raws {
		pos := rp.Position
		var currentPrice, marketValue, costBasis float64
		switch pos.PositionType {
		case "stock":
			quote, err := ps.marketService.GetQuote(pos.Symbol)
			if err != nil {
				continue
			}
			currentPrice = quote.Price
			marketValue = currentPrice * float64(pos.Quantity)
			costBasis = pos.AvgCost * float64(pos.Quantity)
		case "call", "put":
			price, err := ps.optionsService.GetCurrentOptionPrice(pos.Symbol)
			if err != nil {
				continue
			}
			currentPrice = price
			marketValue = currentPrice * float64(pos.Quantity) * 100
			costBasis = pos.AvgCost * float64(pos.Quantity) * 100
		default:
			continue
		}

		positions = append(positions, models.PositionWithValue{
			Position:      pos,
			CurrentPrice:  currentPrice,
			MarketValue:   marketValue,
			UnrealizedPnL: marketValue - costBasis,
		})
		totalPositionValue += marketValue
	}

	totalValue := cashBalance + totalPositionValue
	totalPnL := totalValue - startingBalance
	totalPnLPct := 0.0
	if startingBalance > 0 {
		totalPnLPct = (totalPnL / startingBalance) * 100
	}

	return &Portfolio{
		BotID:       bot.ID,
		BotName:     bot.Name,
		SeasonID:    seasonID,
		CashBalance: cashBalance,
		Positions:   positions,
		TotalValue:  totalValue,
		TotalPnL:    totalPnL,
		TotalPnLPct: totalPnLPct,
	}, nil
}

// parsePositionTime accepts both RFC3339 (what newer code writes) and the
// SQLite default DATETIME format ("2006-01-02 15:04:05"), so old rows and
// new rows both parse cleanly.
func parsePositionTime(s sql.NullString) time.Time {
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
