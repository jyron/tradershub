# API Functional Test Plan

These tests should mimic a real API user as much as possible: call HTTP routes,
use `X-API-Key`, create real runs, advance the simulator, and verify the JSON
contract. Test data should live in isolated temporary databases so cleanup is
automatic and no test keys, bots, runs, or leaderboard rows remain in shared
databases.

## 1. API Key Creation

- `POST /api/v1/keys` with no auth creates a free key.
- Optional `name` and `email` are trimmed and persisted.
- Response includes `api_key`, `key_id`, `name`, and `plan=free`.
- Missing `X-API-Key` fails on protected routes.
- Invalid, inactive, or disabled API keys fail with the correct status.

## 2. Scenario Discovery And Run Start

- Public `GET /api/v1/scenarios` returns ready or archived scenarios only.
- Public `GET /api/v1/scenarios/{slug-or-id}` returns scenario rules and universe.
- `POST /api/v1/runs` starts a run by `scenario_slug`.
- `POST /api/v1/runs` starts a run by `scenario_id`.
- Missing scenario, draft scenario, or scenario with no bars fails cleanly.
- Run creation enforces ownership, plan quota, starting cash, scenario version,
  initial `sim_time`, and optional `bot_name`.

## 3. Run Loop Behaviors

- `GET /api/v1/runs/{id}` returns run, positions, queued orders, and last equity.
- `GET /api/v1/runs/{id}/market` returns only bars at or before current `sim_time`.
- Market `symbols` and `lookback` filters work and do not leak future bars.
- `POST /api/v1/runs/{id}/trades` queues valid `buy`, `sell`, `short`, and `cover`
  orders.
- Invalid symbols, invalid sides, non-positive quantities, insufficient buying
  power, selling without a long position, and covering without a short position
  return user-readable 400 responses.
- Trade idempotency returns the same response for the same key and same body.
- Reusing an idempotency key with a different body returns 409.
- `POST /api/v1/runs/{id}/step` defaults to one bar and respects explicit counts.
- A step fills queued orders at the next bar open with slippage, updates cash,
  positions, equity, and clears filled queued orders.
- Results are rejected while the run is still active.
- A sharp adverse move can liquidate a leveraged run, close positions, stop the
  loop, and produce liquidated results.
- Owners cannot read or mutate another key's run.
- Further steps after `completed` or `liquidated` fail cleanly.

## 4. Completion, Results, And Submission

- A large final step completes the run once the scenario timeline is exhausted.
- `GET /api/v1/runs/{id}/results` computes and returns finite final equity,
  return percent, drawdown/volatility fields when available, trade count, and
  liquidation status.
- Results are idempotent when fetched repeatedly.
- `POST /api/v1/runs/{id}/publish` computes results if needed and upserts the
  leaderboard row.
- Republishing the same run is safe.
- Public `GET /api/v1/runs/{id}/public` works only after publish.

## 5. Plans And Billing

- Each plan is blocked at the allowance configured by the application, with the
  documented upgrade or limit response for that tier. Tests should read plan
  limits from the same application source as the handler instead of copying
  numeric allowances into fixtures.
- A successful Stripe checkout/session/webhook activation updates the account
  to the selected `plan=pro` or `plan=max` tier.
- `GET /api/v1/billing/account` reflects plan, subscription status, billing
  email, and handle.
- Pro and Max accounts can set a valid handle; invalid or duplicate handles are rejected.
- A paid tier with an available upgrade receives the correct upgrade path; the
  highest configured tier receives the top-tier limit response.
- Customer portal requires a Stripe-managed subscription.
- Subscription cancellation downgrades the account to free; past due keeps the
  current paid plan but records `subscription_status=past_due`.

## 6. Cleanup Strategy

- Integration tests use temp SQLite/libsql files for both the app DB and market
  DB.
- Migrations run against those temp DBs.
- Test scenarios and bars are inserted into those temp DBs only.
- `database.DB` and `database.MarketDB` are restored after each test.
- Closing the DB handles and removing the temp directory cleans all API keys,
  runs, orders, trades, results, and leaderboard rows.
