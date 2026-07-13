package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// App DB — API keys, scenarios catalog, runs, results, leaderboard.
	TursoDatabaseURL string
	TursoAuthToken   string

	// Market DB — historical bars + immutable scenario_bars. Separate Turso DB.
	MarketTursoURL   string
	MarketTursoToken string

	// Alpaca creds, used by the hourly bar-ingest job.
	AlpacaAPIKey    string
	AlpacaSecretKey string

	// Stripe billing.
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeProPriceID    string
	StripeMaxPriceID    string
	// Optional repeating coupon used by the limited founding-builder checkout.
	// The coupon duration and amount are owned by Stripe.
	StripeFoundingCouponID string
	// Previous Max price IDs remain valid for existing subscriptions after a
	// pricing change. Configure as a comma-separated list.
	StripeLegacyMaxPriceIDs []string
	// Optional billing-portal configuration that enables Pro <-> Max plan
	// switching. Empty falls back to the Stripe dashboard's default portal.
	StripePortalConfigID string

	// Transactional email (Resend). Empty API key disables sending — safe for
	// local dev and tests.
	ResendAPIKey string
	MailFrom     string
	MailReplyTo  string

	// Base URL the app is served from. Used to build Stripe success/cancel/return
	// URLs. Set to http://localhost:3000 for local dev, https://bot-trade.org in prod.
	AppBaseURL string

	// AppEncryptionKey (APP_ENCRYPTION_KEY) is a 32-byte hex secret used to
	// encrypt API keys at rest (AES-256-GCM). Held in the environment, never in
	// the DB. Required in production; never rotate without re-encrypting.
	AppEncryptionKey string

	// OAuth login providers for hosted MCP connectors.
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string

	// PostHog product analytics. The project token (phc_…) is a public,
	// client-embeddable key — the same one used in the browser snippet — so it
	// ships as a default and is overridable per environment.
	PostHogAPIKey   string
	PostHogEndpoint string

	Port string
}

func Load() *Config {
	godotenv.Load(".env.local", ".env")

	return &Config{
		TursoDatabaseURL:        os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:          os.Getenv("TURSO_AUTH_TOKEN"),
		MarketTursoURL:          os.Getenv("TURSO_MARKET_DATABASE_URL"),
		MarketTursoToken:        os.Getenv("TURSO_MARKET_AUTH_TOKEN"),
		AlpacaAPIKey:            os.Getenv("ALPACA_API_KEY"),
		AlpacaSecretKey:         os.Getenv("ALPACA_SECRET_KEY"),
		StripeSecretKey:         os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:     os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeProPriceID:        os.Getenv("STRIPE_PRO_PRICE_ID"),
		StripeMaxPriceID:        os.Getenv("STRIPE_MAX_PRICE_ID"),
		StripeFoundingCouponID:  os.Getenv("STRIPE_FOUNDING_COUPON_ID"),
		StripeLegacyMaxPriceIDs: splitCSV(os.Getenv("STRIPE_LEGACY_MAX_PRICE_IDS")),
		StripePortalConfigID:    os.Getenv("STRIPE_PORTAL_CONFIG_ID"),
		ResendAPIKey:            os.Getenv("RESEND_API_KEY"),
		MailFrom:                "BotTrade <jyron@bot-trade.org>",
		MailReplyTo:             "jyron@bot-trade.org",
		AppBaseURL:              getEnv("APP_BASE_URL", "https://bot-trade.org"),
		AppEncryptionKey:        os.Getenv("APP_ENCRYPTION_KEY"),
		GoogleOAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GitHubOAuthClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		PostHogAPIKey:           getEnv("POSTHOG_API_KEY", "phc_wpKjBjqE88hkyBUPLFoowVgeuE8cnhV5MwmE4eUDszw6"),
		PostHogEndpoint:         getEnv("POSTHOG_HOST", "https://us.i.posthog.com"),
		Port:                    getEnv("PORT", "3000"),
	}
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
