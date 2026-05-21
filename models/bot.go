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
	// Tier is the leaderboard tier: "challenger" | "verified" | "official".
	// Drives badges, default leaderboard filter, and runner eligibility.
	Tier string `json:"tier,omitempty"`
}

type RegisterBotRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	CreatorEmail  string `json:"creator_email"`
	IsTest        bool   `json:"is_test,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	IsOfficial    bool   `json:"is_official,omitempty"`
	// IsBaseline marks a deterministic reference bot (SPY buy-and-hold,
	// equal-weight, random walker). Set only by scripts/seed_baselines.py.
	IsBaseline bool `json:"is_baseline,omitempty"`
}

type RegisterBotResponse struct {
	BotID           uuid.UUID `json:"bot_id"`
	APIKey          string    `json:"api_key"`
	ClaimURL        string    `json:"claim_url"`
	StartingBalance float64   `json:"starting_balance"`
}

// SubmitBotRequest is what the hosted-bot submission form posts. The
// LLM API key is encrypted at rest via services.Keyvault and never echoed
// back to clients.
type SubmitBotRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ContactEmail string `json:"contact_email"`
	// Provider tag: "openai_compat" | "anthropic" | "google" | "xai".
	// "openai_compat" covers OpenAI itself plus any OpenAI-compatible
	// endpoint (xAI, OpenRouter, local llama.cpp, etc.).
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	// BaseURL is optional; defaults are applied per-provider in the runner.
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key"`
}

type SubmitBotResponse struct {
	BotID           uuid.UUID `json:"bot_id"`
	APIKey          string    `json:"api_key"`
	BackfillJobID   string    `json:"backfill_job_id"`
	PublicURL       string    `json:"public_url"`
	StartingBalance float64   `json:"starting_balance"`
}
