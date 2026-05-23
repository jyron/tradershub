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

// botContextKey is the unique key under which the authenticated bot is
// stashed on the huma operation context. Defined as an unexported type so
// nothing outside this package can collide.
type botContextKey struct{}

// authMiddleware validates X-API-Key against the `bots` table and stashes
// the bot row on the context. Returns 403 if the row has a non-empty
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

		var bot models.Bot
		var botIDStr, createdAt string
		var isActive int
		var description, creatorEmail, tier, disabledReason sql.NullString
		err := database.DB.QueryRow(
			`SELECT id, name, api_key, description, creator_email,
			        created_at, is_active, COALESCE(tier,''), disabled_reason
			   FROM bots
			  WHERE api_key = ?1 AND is_active = 1`,
			apiKey,
		).Scan(
			&botIDStr, &bot.Name, &bot.APIKey, &description,
			&creatorEmail, &createdAt, &isActive, &tier, &disabledReason,
		)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "Invalid API key")
			return
		}
		bot.Description = description.String
		bot.CreatorEmail = creatorEmail.String

		bot.ID, err = uuid.Parse(botIDStr)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError,
				"invalid bot ID format")
			return
		}
		bot.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		bot.IsActive = isActive != 0
		bot.Tier = tier.String

		if disabledReason.Valid && disabledReason.String != "" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden,
				"Bot is disabled: "+disabledReason.String)
			return
		}

		ctx = huma.WithValue(ctx, botContextKey{}, bot)
		next(ctx)
	}
}

// isPublicRead returns true for huma operations that should bypass the
// X-API-Key check. Today: GET /v1/scenarios and GET /v1/scenarios/:id.
func isPublicRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	if path == "/v1/scenarios" {
		return true
	}
	if strings.HasPrefix(path, "/v1/scenarios/") && !strings.ContainsRune(path[len("/v1/scenarios/"):], '/') {
		return true
	}
	return false
}

// botFrom extracts the authenticated bot from the operation context.
// Returns the zero bot if the middleware didn't run (which would be a
// programming error — every operation should be behind authMiddleware).
func botFrom(ctx context.Context) models.Bot {
	v := ctx.Value(botContextKey{})
	if v == nil {
		return models.Bot{}
	}
	return v.(models.Bot)
}
