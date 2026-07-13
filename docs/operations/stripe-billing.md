# Stripe billing operations

This guide documents the billing integration without treating today's prices,
allowances, or migration choices as permanent product rules.

## Configuration

The billing variables are listed in [configuration.md](configuration.md).
`STRIPE_PRO_PRICE_ID` and `STRIPE_MAX_PRICE_ID` select the active recurring
Prices used for new checkouts. Existing subscriptions remain attached to their
Stripe Price until deliberately migrated.

## Changing a plan price

Stripe Price amounts are immutable. When the public amount changes:

1. Create a new recurring Stripe Price in the correct mode and currency.
2. Update the corresponding Railway environment variable to its Price ID.
3. Decide explicitly whether existing subscriptions keep their current Price
   or move to the new one; do not infer that choice from the public price.
4. Preserve webhook mapping for any legacy Price IDs that still have active
   subscriptions.
5. Update the Customer Portal's allowed products and prices.
6. Update `/pricing` and any user-facing transactional copy that states an
   amount.
7. Run the Stripe E2E test and verify Checkout, webhook processing, account
   state, and the portal in the target Stripe mode.

The active Stripe objects and deployment environment are authoritative. Do not
store live Price IDs in repository documentation.

## Local webhook testing

Install the Stripe CLI, then run:

```bash
stripe listen --forward-to localhost:3000/api/v1/billing/webhook
```

Use the signing secret printed by that process as `STRIPE_WEBHOOK_SECRET` for
the local application.

## Current implementation behavior

- Checkout writes the account/API-key identifier into session and subscription
  metadata so subscription webhooks can resolve the BotTrade account.
- Subscription handlers also fall back to the stored Stripe Customer ID.
- Checkout session verification re-fetches the session from Stripe and expands
  the Customer before applying account state.
- The portal return path is `/pricing`.
- Run usage is counted on UTC calendar-month boundaries.
- Abandoned Checkout sessions can leave unused Stripe Customer objects.

These statements describe code behavior. If the integration changes, update
this guide alongside `handlers/apiv1/billing.go` and its tests.

## Verification

The default Go suite does not contact Stripe. Live test-mode coverage is in
`stripee2e/` and requires the `stripe_live` build tag and test-mode credentials.
See `stripee2e/README.md` for the command and safety guard.
