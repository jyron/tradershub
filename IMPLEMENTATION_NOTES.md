# Implementation Notes — Stripe Billing

## Required environment variables

| Variable | Description |
|---|---|
| `STRIPE_SECRET_KEY` | Stripe secret key (sk_test_... for test mode, sk_live_... for live) |
| `STRIPE_WEBHOOK_SECRET` | Webhook signing secret from the Stripe dashboard or stripe CLI (whsec_...) |
| `STRIPE_PRO_PRICE_ID` | Stripe Price ID for the $19.99/month Pro plan (price_...) |

Add these to `.env` for local dev. Railway service variables for production.

## Local webhook testing

Install the Stripe CLI, then:

```
stripe listen --forward-to localhost:3000/api/v1/billing/webhook
```

The CLI prints a `whsec_...` secret on startup — use that value as `STRIPE_WEBHOOK_SECRET` during local dev.

## Decisions not fully specified

- **Checkout session + pre-existing customer**: A new Stripe Customer is created on every checkout request where the API key does not already have a Stripe-managed subscription. If the user abandons checkout, they accumulate orphaned Customer objects in Stripe. This is standard practice; Stripe cleans up failed sessions.

- **`checkout.session.completed` re-fetch**: The webhook re-fetches the session from Stripe with `customer` expanded rather than parsing from the raw webhook body. This is the recommended approach because Stripe does not expand nested objects in webhook payloads by default.

- **`customer.subscription.*` events before `checkout.session.completed`**: Checkout writes `api_key_id` into both session metadata and subscription metadata. Subscription events update that `api_keys` row directly; if metadata is missing, the handler falls back to `stripe_customer_id`.

- **Billing portal return URL**: Set to `/pricing`. Stripe requires a return URL; `/pricing` is the most logical destination.

- **`getBillingSession` endpoint (success page)**: Verifies `payment_status == "paid"` on the Stripe session, applies the same API-key upgrade as the webhook, then returns the Pro API key. No separate "known sessions" table — the Stripe session ID is unguessable and Stripe validates it server-side.

- **Quota counting**: Counts all runs since `datetime('now', 'start of month')` in SQLite/libsql, which uses UTC. This matches the "UTC month boundaries" spec.

- **`huma.Error402PaymentRequired`**: Verified present in huma v2.38.0.

- **Stripe SDK version**: `github.com/stripe/stripe-go/v82 v82.5.1` — highest major, latest stable as of 2026-05-24.

- **`stripe.Key` assignment**: Set per-handler call rather than globally at startup to avoid a race if the key is ever rotated without restart. Acceptable for a single-process server; for high concurrency a once-set global would also be fine.

## What was NOT built (per spec)

Email verification, magic links, password auth, dashboard, team plans, annual billing, badges, tournaments, early access, run history exports.
