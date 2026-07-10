# Email notifications + Max plan — design

Date: 2026-07-10. Approved by jyron.

## Goal

Convert signups into paying customers with transactional email at the two moments that matter, and give Pro users a paid ceiling to grow into.

1. Welcome email on OAuth signup.
2. Upgrade email when a free account hits its 25-run month quota (pitch Pro, $19.99/200).
3. Upgrade email when a Pro account hits its 200-run quota (pitch Max, $79.99/1000).
4. New Max plan: 1000 runs/month, $79.99/mo, via Stripe.
5. Pro quota exhaustion returns HTTP 402 Payment Required (was 429) with a real checkout path to Max. Max exhaustion returns 429 (top tier).

## Architecture

In-process, event-driven. No queue, no cron, no external automation tool.

- `services/mailer.go` — Resend HTTP API client (stdlib `net/http`, no SDK). `RESEND_API_KEY` unset → log and skip (local/dev safe). All sends fire-and-forget goroutines; failures logged, never returned to the API caller.
- Sender: `BotTrade <hello@bot-trade.org>` (Resend domain verification on bot-trade.org).
- Dedupe: migration `009_email_log.sql` — `email_log(account_id, kind, period, sent_at, PRIMARY KEY(account_id, kind, period))`. INSERT OR IGNORE before send; row exists → skip. Welcome uses period `''` (once ever); quota emails use UTC `YYYY-MM` (once per month per kind).

## Triggers

- Welcome: `handlers/apiv1/oauth.go` account-creation INSERT (~line 725). Email known from Google/GitHub profile. Content: 25 free runs, copy-paste first run command, MCP URL, /demo /challenge /account links. Never include the API key (secret).
- Free quota hit: `handlers/apiv1/runs.go` free-402 path in `enforceRunQuota`.
- Pro quota hit: same function, pro path (now 402).

## Max plan changes

- Env `STRIPE_MAX_PRICE_ID` alongside `STRIPE_PRO_PRICE_ID`; new $79.99/mo Stripe price created via API.
- `billingCheckout` accepts optional `plan` ("pro" default, "max").
- Stripe webhook derives plan from the subscription's price ID instead of assuming "pro".
- Quota limits: free 25, pro 200, max 1000.
- Fix existing bug: free-402 `checkout_url` returns a literal placeholder string (`runs.go:181`); becomes a real pointer (pricing page / checkout endpoint).
- `static/pricing.html`: Max card. Account page: plan-aware upgrade action.

## Email copy rules

Plain English, short. No "frozen" (say historic). State price once, plainly. No fake urgency. Reply-to reaches a human.

## Testing

- Go: quota tier matrix (free/pro/max × under/at limit), webhook price→plan mapping, dedupe (second send same period skipped), mailer against `httptest` server.
- Live: real signup → welcome email received; quota exhaustion path exercised against prod after deploy.

## Ops (user-owned)

- Create Resend account, verify bot-trade.org (DNS records), set `RESEND_API_KEY` in Railway.
- `STRIPE_MAX_PRICE_ID` set in Railway after price creation.
