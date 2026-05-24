# Implementation Notes — Stripe Billing

## Required environment variables

| Variable | Description |
|---|---|
| `STRIPE_SECRET_KEY` | Stripe secret key (sk_test_... for test mode, sk_live_... for live) |
| `STRIPE_WEBHOOK_SECRET` | Webhook signing secret from the Stripe dashboard or stripe CLI (whsec_...) |
| `STRIPE_PRO_PRICE_ID` | Stripe Price ID for the $39/month Pro plan (price_...) |

Add these to `.env` for local dev. Railway service variables for production.

## Local webhook testing

Install the Stripe CLI, then:

```
stripe listen --forward-to localhost:3000/api/v1/billing/webhook
```

The CLI prints a `whsec_...` secret on startup — use that value as `STRIPE_WEBHOOK_SECRET` during local dev.

## Decisions not fully specified

- **Checkout session + pre-existing customer**: A new Stripe Customer is created on every checkout request where the bot is not already on an active account. If the user abandons checkout, they accumulate orphaned Customer objects in Stripe. This is standard practice; Stripe cleans up failed sessions.

- **`checkout.session.completed` re-fetch**: The webhook re-fetches the session from Stripe with `customer` expanded rather than parsing from the raw webhook body. This is the recommended approach because Stripe does not expand nested objects in webhook payloads by default.

- **`customer.subscription.*` events before `checkout.session.completed`**: The subscription upsert handler will create a skeleton account row (empty email, fresh account_token) if no row exists yet. When `checkout.session.completed` arrives it fills in the email via `COALESCE(NULLIF(accounts.email, ''), excluded.email)`. The account_token on the skeleton row is what matters; the checkout handler does not overwrite it on conflict.

- **Billing portal return URL**: Set to `/pricing`. Stripe requires a return URL; `/pricing` is the most logical destination.

- **`getBillingSession` endpoint (success page)**: Verifies `payment_status == "paid"` on the Stripe session, then looks up the account by `stripe_customer_id`. No separate "known sessions" table — the Stripe session ID is unguessable and Stripe validates it server-side.

- **Quota counting**: Counts all runs since `datetime('now', 'start of month')` in SQLite/libsql, which uses UTC. This matches the "UTC month boundaries" spec.

- **`huma.Error402PaymentRequired`**: Verified present in huma v2.38.0.

- **Stripe SDK version**: `github.com/stripe/stripe-go/v82 v82.5.1` — highest major, latest stable as of 2026-05-24.

- **`stripe.Key` assignment**: Set per-handler call rather than globally at startup to avoid a race if the key is ever rotated without restart. Acceptable for a single-process server; for high concurrency a once-set global would also be fine.

## What was NOT built (per spec)

Email verification, magic links, password auth, dashboard, team plans, annual billing, badges, tournaments, early access, run history exports.
