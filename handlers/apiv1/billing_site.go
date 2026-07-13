package apiv1

import (
	"bottrade/analytics"

	"net/http"

	"github.com/gofiber/fiber/v2"
	stripe "github.com/stripe/stripe-go/v82"
)

// mountBillingSite registers cookie-authenticated billing routes for the
// marketing site. They mirror the /api/v1/billing/* API operations but
// authenticate via the bt_session cookie set by OAuth login, so a human user
// never has to handle their own API key to subscribe or manage their plan.
func (h *handlers) mountBillingSite(app *fiber.App) {
	app.Post("/billing/checkout", h.siteBillingCheckout)
	app.Post("/billing/portal", h.siteBillingPortal)
	app.Get("/billing/success", h.siteBillingSuccess)
}

func (h *handlers) siteBillingCheckout(c *fiber.Ctx) error {
	accountID, err := h.siteSessionAccountID(c)
	if err != nil || accountID == "" {
		return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "not signed in"})
	}
	key, err := loadAPIKeyByAccountID(accountID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load api key"})
	}
	plan, err := normalizeCheckoutPlan(c.Query("plan"))
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	offer, err := normalizeCheckoutOffer(c.Query("offer"), plan)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	url, _, err := h.createCheckoutOrPortalURL(key, plan, offer)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	h.Analytics.Capture(accountID, "billing_checkout_started", analytics.Props().
		Set("plan", key.Plan).
		Set("target_plan", plan).
		Set("offer", offer).
		Set("flow", "site").
		Set("$ip", clientIP(c)))
	return c.JSON(fiber.Map{"url": url})
}

func (h *handlers) siteBillingPortal(c *fiber.Ctx) error {
	accountID, err := h.siteSessionAccountID(c)
	if err != nil || accountID == "" {
		return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "not signed in"})
	}
	key, err := loadAPIKeyByAccountID(accountID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load api key"})
	}
	if key.StripeCustomerID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "no Stripe customer for this account"})
	}
	url, err := h.createPortalSession(key.StripeCustomerID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"url": url})
}

// siteBillingSuccess is the Stripe Checkout success-URL landing route. It
// activates the account from the Checkout Session synchronously (so the user
// sees Pro on /account even before the webhook arrives) and redirects to
// /account. If the session is missing or unpaid, redirects to /pricing.
func (h *handlers) siteBillingSuccess(c *fiber.Ctx) error {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		return c.Redirect("/pricing", http.StatusFound)
	}
	stripe.Key = h.StripeSecretKey
	cs, err := h.retrieveCheckoutSession(sessionID)
	if err == nil && cs.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
		_ = h.applyCheckoutSession(cs)
	}
	return c.Redirect("/account", http.StatusFound)
}
