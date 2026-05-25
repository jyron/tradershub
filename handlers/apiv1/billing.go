package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"
	stripe "github.com/stripe/stripe-go/v82"
	stripebillingportalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/webhook"
)

var handleRe = regexp.MustCompile(`^[A-Za-z0-9_-]{3,24}$`)

// mountBillingWebhook registers the Stripe webhook receiver as a plain Fiber
// route so auth middleware never touches it and Stripe signatures can be
// verified against the raw request body.
func (h *handlers) mountBillingWebhook(app *fiber.App) {
	app.Post("/api/v1/billing/webhook", h.stripeWebhook)
}

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
		Summary:     "Get account billing info",
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
		Summary:     "Retrieve API key after a completed checkout",
		Tags:        []string{"Billing"},
	}, h.getBillingSession)
}

type CheckoutInput struct{}

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
		AccountID          string `json:"account_id"`
		KeyID              string `json:"key_id"`
		Name               string `json:"name"`
		Plan               string `json:"plan"`
		BillingEmail       string `json:"billing_email,omitempty"`
		SubscriptionStatus string `json:"subscription_status,omitempty"`
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
		APIKey             string `json:"api_key"`
		KeyID              string `json:"key_id"`
		Plan               string `json:"plan"`
		SubscriptionStatus string `json:"subscription_status,omitempty"`
	}
}

func (h *handlers) billingCheckout(ctx context.Context, _ *CheckoutInput) (*CheckoutOutput, error) {
	stripe.Key = h.StripeSecretKey

	key := apiKeyFrom(ctx)

	if key.StripeCustomerID != "" && key.StripeSubscriptionID != "" && key.Plan == "pro" {
		ps, err := h.createPortalSession(key.StripeCustomerID)
		if err != nil {
			return nil, err
		}
		return nil, huma.Error409Conflict("already subscribed — manage here: " + ps)
	}

	metadata := map[string]string{
		"account_id": key.AccountID.String(),
		"api_key_id": key.ID.String(),
	}
	csParams := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(h.StripeProPriceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(h.AppBaseURL + "/billing/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(h.AppBaseURL + "/pricing"),
		Metadata:   metadata,
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: metadata,
		},
	}
	if key.StripeCustomerID != "" {
		csParams.Customer = stripe.String(key.StripeCustomerID)
	} else if key.CreatorEmail != "" {
		csParams.CustomerEmail = stripe.String(key.CreatorEmail)
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
	key := apiKeyFrom(ctx)
	stripe.Key = h.StripeSecretKey

	if key.StripeCustomerID == "" || key.StripeSubscriptionID == "" {
		return nil, huma.Error402PaymentRequired("this account does not have a Stripe-managed subscription")
	}

	url, err := h.createPortalSession(key.StripeCustomerID)
	if err != nil {
		return nil, err
	}
	out := &PortalOutput{}
	out.Body.URL = url
	return out, nil
}

func (h *handlers) createPortalSession(stripeCustomerID string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(h.AppBaseURL + "/pricing"),
	}
	ps, err := stripebillingportalsession.New(params)
	if err != nil {
		return "", huma.NewError(http.StatusInternalServerError, "failed to create portal session: "+err.Error())
	}
	return ps.URL, nil
}

func (h *handlers) getBillingAccount(ctx context.Context, _ *struct{}) (*AccountOutput, error) {
	key := apiKeyFrom(ctx)
	out := &AccountOutput{}
	out.Body.AccountID = key.AccountID.String()
	out.Body.KeyID = key.ID.String()
	out.Body.Name = key.Name
	out.Body.Plan = key.Plan
	out.Body.BillingEmail = key.BillingEmail
	out.Body.SubscriptionStatus = key.SubscriptionStatus
	out.Body.CurrentPeriodEnd = key.CurrentPeriodEnd
	out.Body.Handle = key.Handle
	return out, nil
}

func (h *handlers) patchBillingAccount(ctx context.Context, in *PatchAccountInput) (*PatchAccountOutput, error) {
	key := apiKeyFrom(ctx)
	if key.Plan != "pro" {
		return nil, huma.Error402PaymentRequired("Pro plan required to set a handle")
	}

	handle := strings.TrimSpace(in.Body.Handle)
	if !handleRe.MatchString(handle) {
		return nil, huma.Error400BadRequest("handle must be 3-24 chars, alphanumeric/underscore/hyphen")
	}

	_, err := database.DB.Exec(
		`UPDATE accounts SET handle = ?1 WHERE id = ?2`,
		handle, key.AccountID.String(),
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

	cs, err := h.retrieveCheckoutSession(in.SessionID)
	if err != nil {
		return nil, err
	}
	if cs.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return nil, huma.Error404NotFound("session not paid")
	}

	accountID := cs.Metadata["account_id"]
	keyID := cs.Metadata["api_key_id"]
	if accountID == "" && keyID == "" {
		return nil, huma.Error404NotFound("session is missing account metadata")
	}
	if err := h.applyCheckoutSession(cs); err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, "failed to activate account: "+err.Error())
	}

	var key models.APIKey
	if accountID != "" {
		key, err = loadAPIKeyByAccountID(accountID)
	} else {
		key, err = loadAPIKeyByID(keyID)
	}
	if err != nil {
		return nil, huma.Error404NotFound("API key not found")
	}

	out := &SessionOutput{}
	out.Body.APIKey = key.Key
	out.Body.KeyID = key.ID.String()
	out.Body.Plan = key.Plan
	out.Body.SubscriptionStatus = key.SubscriptionStatus
	return out, nil
}

func (h *handlers) retrieveCheckoutSession(sessionID string) (*stripe.CheckoutSession, error) {
	csParams := &stripe.CheckoutSessionParams{}
	csParams.AddExpand("customer")
	csParams.AddExpand("subscription")
	cs, err := session.Get(sessionID, csParams)
	if err != nil {
		return nil, huma.Error404NotFound("session not found")
	}
	return cs, nil
}

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

	sessionID := event.GetObjectValue("id")
	if sessionID == "" {
		log.Printf("stripe webhook checkout.session.completed: no session id in event")
		return
	}
	cs, err := h.retrieveCheckoutSession(sessionID)
	if err != nil {
		log.Printf("stripe webhook checkout.session.completed: get session failed: %v", err)
		return
	}
	if err := h.applyCheckoutSession(cs); err != nil {
		log.Printf("stripe webhook checkout.session.completed: apply session failed: %v", err)
	}
}

func (h *handlers) applyCheckoutSession(cs *stripe.CheckoutSession) error {
	accountID := cs.Metadata["account_id"]
	if accountID == "" {
		keyID := cs.Metadata["api_key_id"]
		if keyID != "" {
			_ = database.DB.QueryRow(
				`SELECT COALESCE(account_id, id) FROM api_keys WHERE id = ?1`,
				keyID,
			).Scan(&accountID)
		}
	}
	if accountID == "" {
		return nil
	}

	stripeCustomerID := ""
	if cs.Customer != nil {
		stripeCustomerID = cs.Customer.ID
	}
	stripeSubscriptionID := ""
	subStatus := string(cs.Status)
	var periodEnd string
	if cs.Subscription != nil {
		stripeSubscriptionID = cs.Subscription.ID
		if cs.Subscription.Status != "" {
			subStatus = string(cs.Subscription.Status)
		}
	}

	email := ""
	if cs.CustomerDetails != nil && cs.CustomerDetails.Email != "" {
		email = cs.CustomerDetails.Email
	}
	if email == "" && cs.Customer != nil {
		email = cs.Customer.Email
	}

	plan := planFromSubscriptionStatus(subStatus)
	if plan == "free" && cs.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
		plan = "pro"
	}

	_, err := database.DB.Exec(`
		UPDATE accounts
		   SET plan = ?1,
		       stripe_customer_id = COALESCE(NULLIF(?2, ''), stripe_customer_id),
		       stripe_subscription_id = COALESCE(NULLIF(?3, ''), stripe_subscription_id),
		       subscription_status = COALESCE(NULLIF(?4, ''), subscription_status),
		       current_period_end = COALESCE(NULLIF(?5, ''), current_period_end),
		       billing_email = COALESCE(NULLIF(?6, ''), billing_email)
		 WHERE id = ?7
	`, plan, stripeCustomerID, stripeSubscriptionID, subStatus, periodEnd, email, accountID)
	return err
}

func (h *handlers) handleSubscriptionUpsert(event stripe.Event) {
	var sub struct {
		ID               string            `json:"id"`
		Status           string            `json:"status"`
		Customer         string            `json:"customer"`
		CurrentPeriodEnd float64           `json:"current_period_end"`
		Metadata         map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		log.Printf("stripe webhook subscription upsert: unmarshal failed: %v", err)
		return
	}
	if sub.Customer == "" {
		log.Printf("stripe webhook subscription upsert: no customer_id")
		return
	}

	accountID := sub.Metadata["account_id"]
	if accountID == "" {
		_ = database.DB.QueryRow(
			`SELECT id FROM accounts WHERE stripe_customer_id = ?1`,
			sub.Customer,
		).Scan(&accountID)
	}
	if accountID == "" && sub.Metadata["api_key_id"] != "" {
		_ = database.DB.QueryRow(
			`SELECT COALESCE(account_id, id) FROM api_keys WHERE id = ?1`,
			sub.Metadata["api_key_id"],
		).Scan(&accountID)
	}
	if accountID == "" {
		log.Printf("stripe webhook subscription upsert: no account_id for customer=%s", sub.Customer)
		return
	}

	periodEnd := ""
	if sub.CurrentPeriodEnd > 0 {
		periodEnd = time.Unix(int64(sub.CurrentPeriodEnd), 0).UTC().Format(time.RFC3339)
	}

	_, err := database.DB.Exec(`
		UPDATE accounts
		   SET plan = ?1,
		       stripe_customer_id = ?2,
		       stripe_subscription_id = ?3,
		       subscription_status = ?4,
		       current_period_end = COALESCE(NULLIF(?5, ''), current_period_end)
		 WHERE id = ?6
	`, planFromSubscriptionStatus(sub.Status), sub.Customer, sub.ID, sub.Status, periodEnd, accountID)
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
		`UPDATE accounts
		    SET plan = 'free', subscription_status = 'canceled'
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
		`UPDATE accounts
		    SET plan = 'pro', subscription_status = 'past_due'
		  WHERE stripe_customer_id = ?1`,
		inv.Customer,
	); err != nil {
		log.Printf("stripe webhook invoice.payment_failed: %v", err)
	}
}

func planFromSubscriptionStatus(status string) string {
	switch status {
	case "active", "trialing", "past_due":
		return "pro"
	default:
		return "free"
	}
}

func billingIsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
