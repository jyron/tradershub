package models

import (
	"time"

	"github.com/google/uuid"
)

// Bot is the API principal. One row in the bots table per X-API-Key.
type Bot struct {
	ID             uuid.UUID `json:"bot_id"`
	Name           string    `json:"name"`
	APIKey         string    `json:"api_key,omitempty"`
	Description    string    `json:"description"`
	CreatorEmail   string    `json:"creator_email"`
	CreatedAt      time.Time `json:"created_at"`
	IsActive       bool      `json:"is_active"`
	Tier           string    `json:"tier,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	// AccountID is non-empty when this bot is linked to a billing account.
	AccountID string `json:"account_id,omitempty"`
}
