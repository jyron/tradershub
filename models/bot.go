package models

import (
	"time"

	"github.com/google/uuid"
)

type Bot struct {
	ID            uuid.UUID `json:"bot_id"`
	Name          string    `json:"name"`
	APIKey        string    `json:"api_key,omitempty"`
	Description   string    `json:"description"`
	CreatorEmail  string    `json:"creator_email"`
	CashBalance   float64   `json:"cash_balance"`
	CreatedAt     time.Time `json:"created_at"`
	IsActive      bool      `json:"is_active"`
	Claimed       bool      `json:"claimed"`
	IsTest        bool      `json:"is_test,omitempty"`
	// ModelProvider identifies the underlying LLM vendor: "claude" | "gpt" |
	// "gemini" | "grok" | "meta". Drives the model chip in the UI and the
	// showdown page's provider grouping. Empty for legacy bots.
	ModelProvider string `json:"model_provider,omitempty"`
	// IsOfficial marks the canonical benchmark bots (one per provider) that
	// the showdown page is built around. Official bots are exempt from
	// cleanup scripts.
	IsOfficial bool `json:"is_official,omitempty"`
}

type RegisterBotRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	CreatorEmail  string `json:"creator_email"`
	IsTest        bool   `json:"is_test,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	IsOfficial    bool   `json:"is_official,omitempty"`
}

type RegisterBotResponse struct {
	BotID           uuid.UUID `json:"bot_id"`
	APIKey          string    `json:"api_key"`
	ClaimURL        string    `json:"claim_url"`
	StartingBalance float64   `json:"starting_balance"`
}
