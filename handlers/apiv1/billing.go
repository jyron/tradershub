package apiv1

import (
	"bottrade/database"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v82"
	stripebillingportalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

var handleRe = regexp.MustCompile(`^[A-Za-z0-9_-]{3,24}$`)

// ── Fiber-mounted routes (no huma, no auth middleware) ────────────────────────

// mountBillingWebhook registers the Stripe webhook receiver as a plain Fiber
// route so the auth middleware never touches it and we can read the raw request
// bytes that Stripe signature verification requires.
func (h *handlers) mountBillingWebhook(app *fiber.App) {
	app.Post("/api/v1/billing/webhook", h.stripeWebhook)
}

// ── huma-registered billing endpoints ────────────────────────────────────────

func (h *handlers) registerBilling(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "billingCheckout",
		Method:      http.MethodPost,
		Path:        "/api/v1/billing/checkout",
		Summary:     "Create a Stripe Checkout session for Pro subscription",
		Tags:        []string{"Billing"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.billingCheckout)

	huma.Register(api, huma.Operation{
		OperationID: "billingPortal",
		Method:      http.MethodPost,
		Path:        "/api/v1/billing/portal",
		Summary:     "Open the Stripe Customer Portal",
		Tags:        []string{"Billing"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.billingPortal)

	huma.Register(api, huma.Operation{
		OperationID: "getBillingAccount",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/account",
		Summary:     "Get account info and account_token",
		Tags:        []string{"Billing"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.getBillingAccount)

	huma.Register(api, huma.Operation{
		OperationID: "patchBillingAccount",
		Method:      http.MethodPatch,
		Path:        "/api/v1/billing/account",
		Summary:     "Set leaderboard handle",
		Tags:        []string{"Billing"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, h.patchBillingAccount)

	huma.Register(api, huma.Operation{
		OperationID: "getBillingSession",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/session/{session_id}",
		Summary:     "Retrieve account_token after a completed checkout",
		Tags:        []string{"Billing"},
	}, h.getBillingSession)
}

// ── Request / Response types ─────────────────────────────────────────────────

type CheckoutOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type PortalOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type AccountOutput struct {
	Body struct {
		Email              string `json:"email"`
		AccountToken       string `json:"account_token"`
		SubscriptionStatus string `json:"subscription_status"`
		CurrentPeriodEnd   string `json:"current_period_end,omitempty"`
		Handle             string `json:"handle,omitempty"`
	}
}

type PatchAccountInput struct {
	Body struct {
		Handle string `json:"handle"`
	}
}

type PatchAccountOutput struct {
	Body struct {
		Handle string `json:"handle"`
	}
}

type SessionInput struct {
	SessionID string `path:"session_id"`
}

type SessionOutput struct {
	Body struct {
		AccountToken string `json:"account_token"`
	}
}

// ── Handlers ─────────────────────────────────────────────────────────────────

func (h *handlers) billingCheckout(ctx context.Context, _ *struct{}) (*CheckoutOutput, error) {
	bot := botFrom(ctx)
	stripe.Key = h.StripeSecretKey

	// If this bot is already on an active account, return the portal URL instead.
	if bot.AccountID != "" {
		var stripeCustomerID, subStatus string
		_ = database.DB.QueryRow(
			`SELECT stripe_customer_id, COALESCE(subscription_status,'') FROM accounts WHERE id = ?1`,
			bot.AccountID,
		).Scan(&stripeCustomerID, &subStatus)

		if subStatus == "active" || subStatus == "past_due" {
			params := &stripe.BillingPortalSessionParams{
				Customer:  stripe.String(stripeCustomerID),
				ReturnURL: stripe.String(h.AppBaseURL + "/pricing"),
			}
			ps, err := stripebillingportalsession.New(params)
			if err != nil {
				return nil, huma.NewError(http.StatusInternalServerError, "failed to create portal session: "+err.Error())
			}
			return nil, huma.Error409Conflict("already subscribed — manage here: " + ps.URL)
		}
	}

	// Create a Stripe Customer. If the bot has a creator_email, pre-fill it.
	custParams := &stripe.CustomerParams{}
	if bot.CreatorEmail != "" {
		custParams.Email = stripe.String(bot.CreatorEmail)
	}
	cust, err := customer.New(custParams)
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, "stripe customer create failed: "+err.Error())
	}

	// Build Checkout Session. customer and customer_email are mutually exclusive
	// in the Stripe API, so we only set customer (already created above).
	csParams := &stripe.CheckoutSessionParams{
		Customer: stripe.String(cust.ID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(h.StripeProPriceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(h.AppBaseURL + "/billing/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(h.AppBaseURL + "/pricing"),
		Metadata: map[string]string{
			"bot_id":      bot.ID.String(),
			"api_key_hash": billingHashKey(bot.APIKey),
		},
	}

	cs, err := session.New(csParams)
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, "stripe checkout create failed: "+err.Error())
	}

	out := &CheckoutOutput{}
	out.Body.URL = cs.URL
	return out, nil
}

func (h *handlers) billingPortal(ctx context.Context, _ *struct{}) (*PortalOutput, error) {
	bot := botFrom(ctx)
	stripe.Key = h.StripeSecretKey

	if bot.AccountID == "" {
		return nil, huma.Error402PaymentRequired("no active subscription — POST /api/v1/billing/checkout to subscribe")
	}

	var stripeCustomerID, subStatus string
	if err := database.DB.QueryRow(
		`SELECT stripe_customer_id, COALESCE(subscription_status,'') FROM accounts WHERE id = ?1`,
		bot.AccountID,
	).Scan(&stripeCustomerID, &subStatus); err != nil {
		return nil, huma.Error404NotFound("account not found")
	}

	if subStatus != "active" && subStatus != "past_due" {
		return nil, huma.Error402PaymentRequired("subscription is not active")
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(h.AppBaseURL + "/pricing"),
	}
	ps, err := stripebillingportalsession.New(params)
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, "failed to create portal session: "+err.Error())
	}

	out := &PortalOutput{}
	out.Body.URL = ps.URL
	return out, nil
}

func (h *handlers) getBillingAccount(ctx context.Context, _ *struct{}) (*AccountOutput, error) {
	bot := botFrom(ctx)

	if bot.AccountID == "" {
		return nil, huma.Error404NotFound("bot is not linked to any account")
	}

	var email, accountToken, subStatus string
	var handle, periodEnd sql.NullString
	if err := database.DB.QueryRow(
		`SELECT email, account_token, COALESCE(subscription_status,''), handle, current_period_end
		   FROM accounts WHERE id = ?1`,
		bot.AccountID,
	).Scan(&email, &accountToken, &subStatus, &handle, &periodEnd); err != nil {
		return nil, huma.Error404NotFound("account not found")
	}

	out := &AccountOutput{}
	out.Body.Email = email
	out.Body.AccountToken = accountToken
	out.Body.SubscriptionStatus = subStatus
	out.Body.Handle = handle.String
	if periodEnd.Valid {
		out.Body.CurrentPeriodEnd = periodEnd.String
	}
	return out, nil
}

func (h *handlers) patchBillingAccount(ctx context.Context, in *PatchAccountInput) (*PatchAccountOutput, error) {
	bot := botFrom(ctx)

	if bot.AccountID == "" {
		return nil, huma.Error404NotFound("bot is not linked to any account")
	}

	var subStatus string
	if err := database.DB.QueryRow(
		`SELECT COALESCE(subscription_status,'') FROM accounts WHERE id = ?1`, bot.AccountID,
	).Scan(&subStatus); err != nil {
		return nil, huma.Error404NotFound("account not found")
	}
	if subStatus != "active" && subStatus != "past_due" {
		return nil, huma.Error402PaymentRequired("active subscription required to set a handle")
	}

	handle := in.Body.Handle
	if !handleRe.MatchString(handle) {
		return nil, huma.Error400BadRequest("handle must be 3-24 chars, alphanumeric/underscore/hyphen")
	}

	_, err := database.DB.Exec(
		`UPDATE accounts SET handle = ?1, updated_at = CURRENT_TIMESTAMP WHERE id = ?2`,
		handle, bot.AccountID,
	)
	if err != nil {
		if billingIsDuplicate(err) {
			return nil, huma.Error409Conflict("handle already taken")
		}
		return nil, huma.NewError(http.StatusInternalServerError, "failed to update handle: "+err.Error())
	}

	out := &PatchAccountOutput{}
	out.Body.Handle = handle
	return out, nil
}

func (h *handlers) getBillingSession(ctx context.Context, in *SessionInput) (*SessionOutput, error) {
	stripe.Key = h.StripeSecretKey

	// Retrieve the session from Stripe. The session_id is unguessable
	// (Stripe-issued), so verifying payment_status is sufficient protection
	// against enumeration.
	csParams := &stripe.CheckoutSessionParams{}
	csParams.AddExpand("customer")
	cs, err := session.Get(in.SessionID, csParams)
	if err != nil {
		return nil, huma.Error404NotFound("session not found")
	}
	if cs.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return nil, huma.Error404NotFound("session not paid")
	}
	if cs.Customer == nil {
		return nil, huma.Error404NotFound("session has no customer")
	}

	var accountToken string
	if err := database.DB.QueryRow(
		`SELECT account_token FROM accounts WHERE stripe_customer_id = ?1`,
		cs.Customer.ID,
	).Scan(&accountToken); err != nil {
		return nil, huma.Error404NotFound("account not found — webhook may not have processed yet")
	}

	out := &SessionOutput{}
	out.Body.AccountToken = accountToken
	return out, nil
}

// ── Webhook handler ───────────────────────────────────────────────────────────

func (h *handlers) stripeWebhook(c *fiber.Ctx) error {
	payload := c.Body()
	sig := c.Get("Stripe-Signature")

	event, err := webhook.ConstructEventWithOptions(payload, sig, h.StripeWebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		log.Printf("stripe webhook: signature verification failed: %v", err)
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid signature"})
	}

	log.Printf("stripe webhook: event=%s id=%s", event.Type, event.ID)

	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutCompleted(event)
	case "customer.subscription.created", "customer.subscription.updated":
		h.handleSubscriptionUpsert(event)
	case "customer.subscription.deleted":
		h.handleSubscriptionDeleted(event)
	case "invoice.payment_failed":
		h.handleInvoicePaymentFailed(event)
	}

	return c.SendStatus(http.StatusOK)
}

func (h *handlers) handleCheckoutCompleted(event stripe.Event) {
	stripe.Key = h.StripeSecretKey

	// Re-fetch the session with customer expanded so we have the email.
	sessionID := event.GetObjectValue("id")
	if sessionID == "" {
		log.Printf("stripe webhook checkout.session.completed: no session id in event")
		return
	}

	csParams := &stripe.CheckoutSessionParams{}
	csParams.AddExpand("customer")
	cs, err := session.Get(sessionID, csParams)
	if err != nil {
		log.Printf("stripe webhook checkout.session.completed: get session failed: %v", err)
		return
	}

	stripeCustomerID := ""
	if cs.Customer != nil {
		stripeCustomerID = cs.Customer.ID
	}

	email := ""
	if cs.CustomerDetails != nil && cs.CustomerDetails.Email != "" {
		email = cs.CustomerDetails.Email
	}
	if email == "" && cs.Customer != nil {
		email = cs.Customer.Email
	}

	botID := cs.Metadata["bot_id"]

	if stripeCustomerID == "" || email == "" {
		log.Printf("stripe webhook checkout.session.completed: missing customer_id or email, skipping")
		return
	}

	// Upsert account (idempotent via ON CONFLICT on stripe_customer_id).
	accountID := uuid.New().String()
	accountToken := uuid.New().String()

	_, err = database.DB.Exec(`
		INSERT INTO accounts
		  (id, email, account_token, stripe_customer_id, subscription_status, created_at, updated_at)
		VALUES (?1, ?2, ?3, ?4, NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(stripe_customer_id) DO UPDATE SET
		  email      = COALESCE(NULLIF(accounts.email, ''), excluded.email),
		  updated_at = CURRENT_TIMESTAMP
	`, accountID, email, accountToken, stripeCustomerID)
	if err != nil {
		log.Printf("stripe webhook checkout.session.completed: upsert account failed: %v", err)
		return
	}

	// Reload the account id in case the row already existed.
	var actualAccountID string
	if err := database.DB.QueryRow(
		`SELECT id FROM accounts WHERE stripe_customer_id = ?1`, stripeCustomerID,
	).Scan(&actualAccountID); err != nil {
		log.Printf("stripe webhook: reload account id failed: %v", err)
		return
	}

	// Link the bot to the account. We update unconditionally so that a bot
	// whose previous subscription was canceled and who re-subscribes gets
	// promoted again rather than staying stuck on the old canceled account.
	if botID != "" {
		if _, err := database.DB.Exec(
			`UPDATE bots SET account_id = ?1 WHERE id = ?2`,
			actualAccountID, botID,
		); err != nil {
			log.Printf("stripe webhook: link bot failed: %v", err)
		}
	}

	log.Printf("stripe webhook checkout.session.completed: account=%s bot=%s", actualAccountID, botID)
}

func (h *handlers) handleSubscriptionUpsert(event stripe.Event) {
	// Parse the subscription object from the raw event data.
	var sub struct {
		ID                string  `json:"id"`
		Status            string  `json:"status"`
		Customer          string  `json:"customer"`
		CurrentPeriodEnd  float64 `json:"current_period_end"`
	}
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		log.Printf("stripe webhook subscription upsert: unmarshal failed: %v", err)
		return
	}

	if sub.Customer == "" {
		log.Printf("stripe webhook subscription upsert: no customer_id")
		return
	}

	var periodEnd sql.NullString
	if sub.CurrentPeriodEnd > 0 {
		t := time.Unix(int64(sub.CurrentPeriodEnd), 0).UTC()
		periodEnd = sql.NullString{String: t.Format(time.RFC3339), Valid: true}
	}

	// Upsert the account — subscription events may arrive before checkout.session.completed.
	accountID := uuid.New().String()
	accountToken := uuid.New().String()
	_, err := database.DB.Exec(`
		INSERT INTO accounts
		  (id, email, account_token, stripe_customer_id, stripe_subscription_id, subscription_status, current_period_end, created_at, updated_at)
		VALUES (?1, '', ?2, ?3, ?4, ?5, ?6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(stripe_customer_id) DO UPDATE SET
		  stripe_subscription_id = excluded.stripe_subscription_id,
		  subscription_status    = excluded.subscription_status,
		  current_period_end     = excluded.current_period_end,
		  updated_at             = CURRENT_TIMESTAMP
	`, accountID, accountToken, sub.Customer, sub.ID, sub.Status, periodEnd)
	if err != nil {
		log.Printf("stripe webhook subscription upsert: %v", err)
	}
}

func (h *handlers) handleSubscriptionDeleted(event stripe.Event) {
	var sub struct {
		Customer string `json:"customer"`
	}
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil || sub.Customer == "" {
		log.Printf("stripe webhook subscription deleted: bad payload: %v", err)
		return
	}
	if _, err := database.DB.Exec(
		`UPDATE accounts SET subscription_status = 'canceled', updated_at = CURRENT_TIMESTAMP
		  WHERE stripe_customer_id = ?1`,
		sub.Customer,
	); err != nil {
		log.Printf("stripe webhook subscription deleted: %v", err)
	}
}

func (h *handlers) handleInvoicePaymentFailed(event stripe.Event) {
	var inv struct {
		Customer string `json:"customer"`
	}
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil || inv.Customer == "" {
		log.Printf("stripe webhook invoice.payment_failed: bad payload: %v", err)
		return
	}
	if _, err := database.DB.Exec(
		`UPDATE accounts SET subscription_status = 'past_due', updated_at = CURRENT_TIMESTAMP
		  WHERE stripe_customer_id = ?1`,
		inv.Customer,
	); err != nil {
		log.Printf("stripe webhook invoice.payment_failed: %v", err)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// billingHashKey returns a SHA-256 hex digest of an API key, used as a
// non-reversible identifier in Stripe metadata.
func billingHashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func billingIsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
