# Stripe E2E Tests

This package holds live Stripe sandbox tests. They are intentionally separate
from the default test suite and only compile with the `stripe_live` build tag.

Run them only with Stripe **test-mode** credentials:

```bash
STRIPE_SECRET_KEY=sk_test_... \
STRIPE_PRO_PRICE_ID=price_... \
go test -tags=stripe_live ./stripee2e -count=1
```

The live checkout test:

- creates a temporary local BotTrade account and API key in the test database;
- calls the real `POST /api/v1/billing/checkout` handler;
- creates a real Stripe Checkout Session in test mode;
- verifies the session is subscription-mode, uses `STRIPE_PRO_PRICE_ID`, and
  carries the local `api_key_id` metadata;
- expires the Checkout Session during cleanup;
- deletes the temporary Stripe Customer if Stripe created one.

The test refuses to run with a live-mode `sk_live_` key unless
`BOTTRADE_STRIPE_ALLOW_LIVE=1` is set.
