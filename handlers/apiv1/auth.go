package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// apiKeyContextKey is the unique key under which the authenticated key is
// stashed on the huma operation context. Defined as an unexported type so
// nothing outside this package can collide.
type apiKeyContextKey struct{}

// authMiddleware validates X-API-Key against the `api_keys` table and stashes
// the key row on the context. Returns 403 if the row has a non-empty
// disabled_reason (abuse short-circuit).
//
// GET /v1/scenarios and GET /v1/scenarios/:id are exempt — the scenario
// catalog is public-readable so visitors browsing the marketing site can
// see which scenarios exist, their universes, and their time windows.
func (h *handlers) authMiddleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if isPublicRead(ctx.Method(), ctx.URL().Path) {
			next(ctx)
			return
		}
		apiKey := ctx.Header("X-API-Key")
		if apiKey == "" {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized,
				"X-API-Key header required")
			return
		}

		key, err := loadAPIKeyBySecret(apiKey)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Invalid API key")
			return
		}
		if !key.IsActive {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "API key is inactive")
			return
		}
		if key.DisabledReason != "" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden,
				"API key is disabled: "+key.DisabledReason)
			return
		}

		ctx = huma.WithValue(ctx, apiKeyContextKey{}, key)
		next(ctx)
	}
}

func loadAPIKeyBySecret(secret string) (models.APIKey, error) {
	var key models.APIKey
	var keyIDStr, createdAt string
	var isActive int
	var description, creatorEmail, disabledReason sql.NullString
	var stripeCustomerID, stripeSubscriptionID, subStatus sql.NullString
	var currentPeriodEnd, billingEmail, handle sql.NullString
	err := database.DB.QueryRow(
		`SELECT id, name, api_key, description, creator_email,
		        created_at, is_active, disabled_reason, plan,
		        stripe_customer_id, stripe_subscription_id,
		        subscription_status, current_period_end, billing_email, handle
		   FROM api_keys
		  WHERE api_key = ?1`,
		secret,
	).Scan(
		&keyIDStr, &key.Name, &key.Key, &description,
		&creatorEmail, &createdAt, &isActive, &disabledReason, &key.Plan,
		&stripeCustomerID, &stripeSubscriptionID, &subStatus,
		&currentPeriodEnd, &billingEmail, &handle,
	)
	if err != nil {
		return key, err
	}
	key.Description = description.String
	key.CreatorEmail = creatorEmail.String
	key.DisabledReason = disabledReason.String
	key.StripeCustomerID = stripeCustomerID.String
	key.StripeSubscriptionID = stripeSubscriptionID.String
	key.SubscriptionStatus = subStatus.String
	key.CurrentPeriodEnd = currentPeriodEnd.String
	key.BillingEmail = billingEmail.String
	key.Handle = handle.String

	key.ID, err = uuid.Parse(keyIDStr)
	if err != nil {
		return key, err
	}
	key.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	key.IsActive = isActive != 0
	return key, nil
}

func loadAPIKeyByID(id string) (models.APIKey, error) {
	var secret string
	if err := database.DB.QueryRow(`SELECT api_key FROM api_keys WHERE id = ?1`, id).Scan(&secret); err != nil {
		return models.APIKey{}, err
	}
	return loadAPIKeyBySecret(secret)
}

// isPublicRead returns true for huma operations that should bypass the
// X-API-Key check. Today: GET /v1/scenarios and GET /v1/scenarios/:id.
func isPublicRead(method, path string) bool {
	if method == http.MethodPost && path == "/api/v1/billing/checkout" {
		return true
	}
	if method != http.MethodGet {
		return false
	}
	if path == "/api/v1/scenarios" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/scenarios/") && !strings.ContainsRune(path[len("/api/v1/scenarios/"):], '/') {
		return true
	}
	// /api/v1/billing/session/{session_id} — the session_id is unguessable
	// (from Stripe) and serves as the bearer credential; the success page
	// must call it from the browser without an API key.
	if strings.HasPrefix(path, "/api/v1/billing/session/") {
		return true
	}
	return false
}

// apiKeyFrom extracts the authenticated API key from the operation context.
// Returns the zero bot if the middleware didn't run (which would be a
// programming error — every operation should be behind authMiddleware).
func apiKeyFrom(ctx context.Context) models.APIKey {
	v := ctx.Value(apiKeyContextKey{})
	if v == nil {
		return models.APIKey{}
	}
	return v.(models.APIKey)
}
