package models

import (
	"time"

	"github.com/google/uuid"
)

type Trade struct {
	ID             uuid.UUID  `json:"trade_id"`
	BotID          uuid.UUID  `json:"bot_id"`
	Symbol         string     `json:"symbol"`
	TradeType      string     `json:"trade_type"` // 'stock', 'call', 'put'
	Side           string     `json:"side"`       // 'buy' or 'sell'
	Quantity       int        `json:"quantity"`
	Price          float64    `json:"price"`
	StrikePrice    *float64   `json:"strike_price,omitempty"`
	ExpirationDate *time.Time `json:"expiration_date,omitempty"`
	TotalValue     float64    `json:"total"`
	Reasoning      string     `json:"reasoning,omitempty"`
	ExecutedAt     time.Time  `json:"executed_at"`
}

type StockTradeRequest struct {
	Symbol    string `json:"symbol"`
	Side      string `json:"side"` // 'buy' or 'sell'
	Quantity  int    `json:"quantity"`
	Reasoning string `json:"reasoning"`
}

type TradeResponse struct {
	TradeID    uuid.UUID `json:"trade_id"`
	Status     string    `json:"status"`
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"`
	Quantity   int       `json:"quantity"`
	Price      float64   `json:"price"`
	Total      float64   `json:"total"`
	ExecutedAt time.Time `json:"executed_at"`
}
