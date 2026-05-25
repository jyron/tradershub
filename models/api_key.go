package models

import (
	"time"

	"github.com/google/uuid"
)

// APIKey is the API principal. One key owns a subscription, a quota bucket,
// and any number of runs created by any bot or script using that key.
type APIKey struct {
	ID                   uuid.UUID `json:"key_id"`
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
