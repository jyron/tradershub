# Configuration

`config/config.go` is the authoritative list of application environment
variables and defaults. This page groups them by purpose without copying
secrets, IDs, prices, or other environment-specific values.

## Application and storage

| Variable | Purpose |
|---|---|
| `APP_BASE_URL` | Public origin used for generated callback and return URLs. |
| `APP_ENCRYPTION_KEY` | AES-256-GCM key used to encrypt API keys at rest. Required when running the application. |
| `PORT` | HTTP listen port. |
| `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN` | Application database connection. |
| `TURSO_MARKET_DATABASE_URL`, `TURSO_MARKET_AUTH_TOKEN` | Market database connection. |

Local development falls back to SQLite files when database URLs are absent.

## Integrations

| Variable | Purpose |
|---|---|
| `ALPACA_API_KEY`, `ALPACA_SECRET_KEY` | Market-data ingestion. Ingestion is disabled when absent. |
| `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | Google OAuth sign-in. |
| `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth sign-in. |
| `POSTHOG_API_KEY`, `POSTHOG_HOST` | Product analytics. |
| `RESEND_API_KEY` | Transactional email. Sending is skipped when absent. |

The application sends support replies to `jyron@bot-trade.org`; this is the
only public support contact address.

## Billing

| Variable | Purpose |
|---|---|
| `STRIPE_SECRET_KEY` | Stripe API credential for the selected mode. |
| `STRIPE_WEBHOOK_SECRET` | Signature secret for the billing webhook endpoint. |
| `STRIPE_PRO_PRICE_ID` | Active Stripe Price selected for new Pro checkouts. |
| `STRIPE_MAX_PRICE_ID` | Active Stripe Price selected for new Max checkouts. |
| `STRIPE_LEGACY_MAX_PRICE_IDS` | Previous Max Price IDs that still need webhook-to-plan mapping. |
| `STRIPE_PORTAL_CONFIG_ID` | Optional portal configuration for plan switching. |

Price amounts are owned by Stripe and displayed by `/pricing`; they are not
configuration rules in this documentation. Validate environment changes with
the Stripe E2E test described in `stripee2e/README.md`.
