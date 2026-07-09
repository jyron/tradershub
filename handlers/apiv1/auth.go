package apiv1

import (
	"sync"
	"encoding/hex"
	"crypto/sha256"
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

// authMiddleware validates a BotTrade API key against the `api_keys` table and
// stashes the account-owned credential on the context. Clients can pass the key
// as X-API-Key or Authorization: Bearer <key>.
//
// GET /api/v1/scenarios and GET /api/v1/scenarios/:id are exempt — the scenario
// catalog is public-readable so visitors browsing the marketing site can
// see which scenarios exist, their universes, and their time windows.
func (h *handlers) authMiddleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if isPublicRead(ctx.Method(), ctx.URL().Path) {
			next(ctx)
			return
		}
		apiKey := apiKeySecretFromHeaders(ctx.Header("X-API-Key"), ctx.Header("Authorization"))
		if apiKey == "" {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized,
				"BotTrade API key required")
			return
		}

		key, err := loadAPIKeyBySecret(apiKey)
		if err != nil {
			key, err = loadAPIKeyByOAuthAccessToken(apiKey)
		}
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

		recordUsageEvent(key, "rest", ctx.URL().Path, ctx.Method())
		h.aliasCredential(apiKey, key.AccountID.String())
		ctx = huma.WithValue(ctx, apiKeyContextKey{}, key)
		next(ctx)
	}
}

func apiKeySecretFromHeaders(xAPIKey, authorization string) string {
	if key := strings.TrimSpace(xAPIKey); key != "" {
		return key
	}
	auth := strings.TrimSpace(authorization)
	const bearer = "Bearer "
	if strings.HasPrefix(auth, bearer) {
		return strings.TrimSpace(strings.TrimPrefix(auth, bearer))
	}
	return ""
}

func loadAPIKeyBySecret(secret string) (models.APIKey, error) {
	var key models.APIKey
	var keyIDStr, accountIDStr, createdAt string
	var isActive int
	var description, creatorEmail, disabledReason sql.NullString
	var stripeCustomerID, stripeSubscriptionID, subStatus sql.NullString
	var currentPeriodEnd, billingEmail, handle sql.NullString
	err := database.DB.QueryRow(
		`SELECT k.id,
		        COALESCE(k.account_id, k.id) AS account_id,
		        k.name,
		        k.api_key,
		        k.description,
		        k.creator_email,
		        k.created_at,
		        CASE
		          WHEN k.is_active = 1 AND COALESCE(a.is_active, 1) = 1 THEN 1
		          ELSE 0
		        END AS is_active,
		        COALESCE(NULLIF(a.disabled_reason, ''), k.disabled_reason),
		        COALESCE(NULLIF(a.plan, ''), k.plan),
		        COALESCE(a.stripe_customer_id, k.stripe_customer_id),
		        COALESCE(a.stripe_subscription_id, k.stripe_subscription_id),
		        COALESCE(a.subscription_status, k.subscription_status),
		        COALESCE(a.current_period_end, k.current_period_end),
		        COALESCE(a.billing_email, k.billing_email),
		        COALESCE(a.handle, k.handle)
		   FROM api_keys k
		   LEFT JOIN accounts a ON a.id = k.account_id
		  WHERE k.api_key = ?1`,
		secret,
	).Scan(
		&keyIDStr, &accountIDStr, &key.Name, &key.Key, &description,
		&creatorEmail, &createdAt, &isActive, &disabledReason, &key.Plan,
		&stripeCustomerID, &stripeSubscriptionID, &subStatus,
		&currentPeriodEnd, &billingEmail, &handle,
	)
	if err != nil {
		return key, err
	}
	key.AccountID, err = uuid.Parse(accountIDStr)
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

func loadAPIKeyByAccountID(accountID string) (models.APIKey, error) {
	var secret string
	if err := database.DB.QueryRow(
		`SELECT api_key
		   FROM api_keys
		  WHERE account_id = ?1
		  ORDER BY created_at ASC
		  LIMIT 1`,
		accountID,
	).Scan(&secret); err != nil {
		return models.APIKey{}, err
	}
	return loadAPIKeyBySecret(secret)
}

func loadAPIKeyByOAuthAccessToken(token string) (models.APIKey, error) {
	var accountID, clientID, expiresAt, revokedAt string
	err := database.DB.QueryRow(
		`SELECT account_id, client_id, expires_at, COALESCE(revoked_at, '')
		   FROM oauth_access_tokens
		  WHERE token_hash = ?1`,
		hashToken(token),
	).Scan(&accountID, &clientID, &expiresAt, &revokedAt)
	if err != nil {
		return models.APIKey{}, err
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if revokedAt != "" || time.Now().UTC().After(exp) {
		return models.APIKey{}, sql.ErrNoRows
	}
	key, _, err := ensureAccountAPIKey(accountID, "", "")
	if err != nil {
		return models.APIKey{}, err
	}
	key.OAuthClientID = clientID
	return key, nil
}

func loadAPIKeyByID(id string) (models.APIKey, error) {
	var secret string
	if err := database.DB.QueryRow(`SELECT api_key FROM api_keys WHERE id = ?1`, id).Scan(&secret); err != nil {
		return models.APIKey{}, err
	}
	return loadAPIKeyBySecret(secret)
}

func recordUsageEvent(key models.APIKey, surface, action, method string) {
	_, _ = database.DB.Exec(
		`INSERT INTO usage_events
		   (id, account_id, credential_id, client_id, surface, action, method)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
		uuid.NewString(), key.AccountID.String(), key.ID.String(), key.OAuthClientID, surface, action, method,
	)
}

// isPublicRead returns true for huma operations that should bypass X-API-Key auth.
func isPublicRead(method, path string) bool {
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
// Returns the zero key if the middleware didn't run (which would be a
// programming error — every operation should be behind authMiddleware).
func apiKeyFrom(ctx context.Context) models.APIKey {
	v := ctx.Value(apiKeyContextKey{})
	if v == nil {
		return models.APIKey{}
	}
	return v.(models.APIKey)
}

// aliasCredential links the MCP server's hashed-credential distinct_id
// ("key_" + sha256(credential)[:24]) to the canonical account distinct_id, so
// anonymous-looking MCP tool events join the account's event stream. Sent at
// most once per credential per process.
var (
	aliasedMu   sync.Mutex
	aliasedKeys = map[string]bool{}
)

func (h *handlers) aliasCredential(credential, accountID string) {
	if credential == "" || accountID == "" {
		return
	}
	sum := sha256.Sum256([]byte(credential))
	hashed := "key_" + hex.EncodeToString(sum[:])[:24]
	aliasedMu.Lock()
	seen := aliasedKeys[hashed]
	aliasedKeys[hashed] = true
	aliasedMu.Unlock()
	if seen {
		return
	}
	h.Analytics.Alias(accountID, hashed)
}
