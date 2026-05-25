package models

import (
	"time"

	"github.com/google/uuid"
)

// APIKey is an account-owned BotTrade credential. The account owns plan,
// billing, quota, runs, and public identity; the key is how scripts, agents,
// REST calls, and MCP clients authenticate to that account.
type APIKey struct {
	ID                   uuid.UUID `json:"key_id"`
	AccountID            uuid.UUID `json:"account_id"`
	OAuthClientID        string    `json:"oauth_client_id,omitempty"`
	Name                 string    `json:"name"`
	Key                  string    `json:"api_key,omitempty"`
	Description          string    `json:"description"`
	CreatorEmail         string    `json:"creator_email"`
	CreatedAt            time.Time `json:"created_at"`
	IsActive             bool      `json:"is_active"`
	DisabledReason       string    `json:"disabled_reason,omitempty"`
	Plan                 string    `json:"plan"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
	SubscriptionStatus   string    `json:"subscription_status,omitempty"`
	CurrentPeriodEnd     string    `json:"current_period_end,omitempty"`
	BillingEmail         string    `json:"billing_email,omitempty"`
	Handle               string    `json:"handle,omitempty"`
}
