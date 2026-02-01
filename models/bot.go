package models

import (
	"time"

	"github.com/google/uuid"
)

type Bot struct {
	ID           uuid.UUID `json:"bot_id"`
	Name         string    `json:"name"`
	APIKey       string    `json:"api_key,omitempty"`
	Description  string    `json:"description"`
	CreatorEmail string    `json:"creator_email"`
	CashBalance  float64   `json:"cash_balance"`
	CreatedAt    time.Time `json:"created_at"`
	IsActive     bool      `json:"is_active"`
}

type RegisterBotRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	CreatorEmail string `json:"creator_email"`
}

type RegisterBotResponse struct {
	BotID           uuid.UUID `json:"bot_id"`
	APIKey          string    `json:"api_key"`
	StartingBalance float64   `json:"starting_balance"`
}
