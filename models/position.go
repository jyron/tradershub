package models

import (
	"time"

	"github.com/google/uuid"
)

type Position struct {
	ID             uuid.UUID `json:"id"`
	BotID          uuid.UUID `json:"bot_id"`
	Symbol         string    `json:"symbol"`
	PositionType   string    `json:"type"`
	Quantity       int       `json:"quantity"`
	AvgCost        float64   `json:"avg_cost"`
	StrikePrice    *float64  `json:"strike_price,omitempty"`
	ExpirationDate *string   `json:"expiration_date,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PositionWithValue struct {
	Position
	CurrentPrice   float64 `json:"current_price"`
	MarketValue    float64 `json:"market_value"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
}
