//go:build stripe_live

package stripee2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/price"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var checkoutSessionIDRe = regexp.MustCompile(`cs_(?:test|live)_[A-Za-z0-9]+`)

func TestStripeCheckoutCreatesCleanableSubscriptionSession(t *testing.T) {
	_ = godotenv.Load("../.env.local", "../.env", ".env.local", ".env")

	secret := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	priceID := strings.TrimSpace(os.Getenv("STRIPE_PRO_PRICE_ID"))
	if secret == "" || priceID == "" {
		t.Skip("STRIPE_SECRET_KEY and STRIPE_PRO_PRICE_ID are required")
	}
	if strings.HasPrefix(secret, "sk_live_") && os.Getenv("BOTTRADE_STRIPE_ALLOW_LIVE") != "1" {
		t.Fatal("refusing to run Stripe E2E test with a live key; use test mode or set BOTTRADE_STRIPE_ALLOW_LIVE=1")
	}
	if !strings.HasPrefix(secret, "sk_test_") && os.Getenv("BOTTRADE_STRIPE_ALLOW_LIVE") != "1" {
		t.Fatalf("expected a Stripe test-mode key, got prefix %q", secretPrefix(secret))
	}

	stripe.Key = secret
	proPrice, err := price.Get(priceID, nil)
	if err != nil {
		t.Fatalf("retrieve STRIPE_PRO_PRICE_ID: %v", err)
	}
	if !proPrice.Active {
		t.Fatalf("price %s is not active", priceID)
	}
	if proPrice.Recurring == nil {
		t.Fatalf("price %s is not recurring; billing checkout uses subscription mode", priceID)
	}

	env := newStripeTestEnv(t, secret, priceID)
	email := fmt.Sprintf("stripe-e2e-%s@example.test", strings.ToLower(randomSuffix()))
	cleanupCustomersByEmail(t, email)
	t.Cleanup(func() { cleanupCustomersByEmail(t, email) })

	apiKey, keyID := env.issueTestKey("stripe e2e", email)

	status, checkoutBody := env.request(http.MethodPost, "/api/v1/billing/checkout", apiKey, nil)
	requireStatus(t, status, http.StatusOK, checkoutBody)
	checkoutURL := requireString(t, checkoutBody, "url")
	sessionID := checkoutSessionIDRe.FindString(checkoutURL)
	if sessionID == "" {
		t.Fatalf("could not parse Checkout Session ID from URL %q", checkoutURL)
	}
	t.Cleanup(func() { expireCheckoutSession(t, sessionID) })

	params := &stripe.CheckoutSessionParams{}
	params.AddExpand("customer")
	cs, err := session.Get(sessionID, params)
	if err != nil {
		t.Fatalf("retrieve checkout session %s: %v", sessionID, err)
	}
	if cs.Mode != stripe.CheckoutSessionModeSubscription {
		t.Fatalf("checkout mode = %q, want subscription", cs.Mode)
	}
	if got := cs.Metadata["api_key_id"]; got != keyID {
		t.Fatalf("checkout metadata api_key_id = %q, want %q", got, keyID)
	}
	if cs.SuccessURL != "http://stripe-e2e.example.test/billing/success?session_id={CHECKOUT_SESSION_ID}" {
		t.Fatalf("unexpected success_url %q", cs.SuccessURL)
	}
	if cs.CancelURL != "http://stripe-e2e.example.test/pricing" {
		t.Fatalf("unexpected cancel_url %q", cs.CancelURL)
	}
	if cs.Customer != nil && cs.Customer.ID != "" {
		t.Cleanup(func() { deleteCustomer(t, cs.Customer.ID) })
	}

	lineItems := session.ListLineItems(&stripe.CheckoutSessionListLineItemsParams{
		Session: stripe.String(sessionID),
	})
	var sawProPrice bool
	for lineItems.Next() {
		item := lineItems.LineItem()
		if item.Price != nil && item.Price.ID == priceID {
			sawProPrice = true
		}
	}
	if err := lineItems.Err(); err != nil {
		t.Fatalf("list line items: %v", err)
	}
	if !sawProPrice {
		t.Fatalf("checkout session %s did not include price %s", sessionID, priceID)
	}

	status, accountBody := env.request(http.MethodGet, "/api/v1/billing/account", apiKey, nil)
	requireStatus(t, status, http.StatusOK, accountBody)
	if got := requireString(t, accountBody, "plan"); got != "free" {
		t.Fatalf("plan before paid checkout = %q, want free", got)
	}
}

type stripeTestEnv struct {
	t     *testing.T
	app   *fiber.App
	appDB *sql.DB
}

func newStripeTestEnv(t *testing.T, stripeSecret, priceID string) *stripeTestEnv {
	t.Helper()

	tmp := t.TempDir()
	appDB := openTestDB(t, filepath.Join(tmp, "app.db"))
	marketDB := openTestDB(t, filepath.Join(tmp, "market.db"))

	oldAppDB := database.DB
	oldMarketDB := database.MarketDB
	database.DB = appDB
	database.MarketDB = marketDB
	t.Cleanup(func() {
		database.DB = oldAppDB
		database.MarketDB = oldMarketDB
		_ = appDB.Close()
		_ = marketDB.Close()
	})

	repoRoot, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if err := database.RunMigrationsOn(appDB, filepath.Join(repoRoot, "database/migrations")); err != nil {
		t.Fatalf("app migrations: %v", err)
	}
	if err := database.RunMigrationsOn(marketDB, filepath.Join(repoRoot, "database/migrations_market")); err != nil {
		t.Fatalf("market migrations: %v", err)
	}

	app := fiber.New(fiber.Config{
		AppName:               "BotTrade Stripe E2E",
		DisableStartupMessage: true,
	})
	apiv1.Mount(app, services.NewScenarioEngine(appDB, marketDB), &config.Config{
		AppBaseURL:       "http://stripe-e2e.example.test",
		StripeSecretKey:  stripeSecret,
		StripeProPriceID: priceID,
	}, nil)
	return &stripeTestEnv{t: t, app: app, appDB: appDB}
}

func (env *stripeTestEnv) issueTestKey(name, email string) (string, string) {
	env.t.Helper()
	accountID := uuid.NewString()
	keyID := uuid.NewString()
	apiKey := "bt_test_" + uuid.NewString()
	if _, err := env.appDB.Exec(
		`INSERT INTO accounts (id, name, email, billing_email, plan)
		 VALUES (?1, ?2, ?3, ?3, 'free')`,
		accountID, name, email,
	); err != nil {
		env.t.Fatalf("insert account: %v", err)
	}
	if _, err := env.appDB.Exec(
		`INSERT INTO api_keys (id, account_id, name, api_key, creator_email, plan)
		 VALUES (?1, ?2, ?3, ?4, ?5, 'free')`,
		keyID, accountID, name, apiKey, email,
	); err != nil {
		env.t.Fatalf("insert api key: %v", err)
	}
	return apiKey, keyID
}

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("%s: %v", pragma, err)
		}
	}
	return db
}

func (env *stripeTestEnv) request(method, path, key string, payload any) (int, map[string]any) {
	env.t.Helper()
	var reader io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			env.t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, reader)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := env.app.Test(req, -1)
	if err != nil {
		env.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		env.t.Fatalf("read response: %v", err)
	}
	if len(bodyBytes) == 0 {
		return resp.StatusCode, map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		env.t.Fatalf("decode response status=%d body=%s: %v", resp.StatusCode, string(bodyBytes), err)
	}
	return resp.StatusCode, decoded
}

func expireCheckoutSession(t *testing.T, sessionID string) {
	t.Helper()
	cs, err := session.Get(sessionID, nil)
	if err != nil {
		t.Logf("cleanup: retrieve checkout session %s: %v", sessionID, err)
		return
	}
	if cs.Status != stripe.CheckoutSessionStatusOpen {
		return
	}
	if _, err := session.Expire(sessionID, nil); err != nil {
		t.Logf("cleanup: expire checkout session %s: %v", sessionID, err)
	}
}

func cleanupCustomersByEmail(t *testing.T, email string) {
	t.Helper()
	iter := customer.List(&stripe.CustomerListParams{
		Email: stripe.String(email),
	})
	for iter.Next() {
		c := iter.Customer()
		deleteCustomer(t, c.ID)
	}
	if err := iter.Err(); err != nil {
		t.Logf("cleanup: list customers by email %s: %v", email, err)
	}
}

func deleteCustomer(t *testing.T, customerID string) {
	t.Helper()
	if customerID == "" {
		return
	}
	if _, err := customer.Del(customerID, nil); err != nil {
		t.Logf("cleanup: delete customer %s: %v", customerID, err)
	}
}

func requireStatus(t *testing.T, got, want int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d; body=%#v", got, want, body)
	}
}

func requireString(t *testing.T, body map[string]any, key string) string {
	t.Helper()
	v, ok := body[key].(string)
	if !ok || v == "" {
		t.Fatalf("%s missing or not string in %#v", key, body)
	}
	return v
}

func randomSuffix() string {
	return strings.ReplaceAll(strings.ToLower(stripe.NewIdempotencyKey()), "_", "")
}

func secretPrefix(secret string) string {
	if len(secret) < 8 {
		return secret
	}
	return secret[:8]
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
