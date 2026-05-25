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
	"testing"
	"time"

	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
	stripe "github.com/stripe/stripe-go/v82"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

func TestStripeWebhookUpdatesAPIKeyAccountState(t *testing.T) {
	const webhookSecret = "whsec_test_secret"
	env := newWebhookTestEnv(t, webhookSecret)

	status, keyBody := env.request(http.MethodPost, "/api/v1/keys", "", nil, map[string]any{
		"name":  "webhook e2e",
		"email": "webhook-e2e@example.test",
	})
	requireWebhookStatus(t, status, http.StatusCreated, keyBody)
	apiKey := requireWebhookString(t, keyBody, "api_key")
	keyID := requireWebhookString(t, keyBody, "key_id")

	activePayload := map[string]any{
		"id":          "evt_test_subscription_updated",
		"object":      "event",
		"api_version": stripe.APIVersion,
		"type":        "customer.subscription.updated",
		"data": map[string]any{
			"object": map[string]any{
				"id":                 "sub_test_webhook",
				"status":             "active",
				"customer":           "cus_test_webhook",
				"current_period_end": time.Now().UTC().Add(30 * 24 * time.Hour).Unix(),
				"metadata": map[string]string{
					"api_key_id": keyID,
				},
			},
		},
	}
	status, webhookBody := env.signedWebhook(webhookSecret, activePayload)
	requireWebhookStatus(t, status, http.StatusOK, webhookBody)

	status, accountBody := env.request(http.MethodGet, "/api/v1/billing/account", apiKey, nil, nil)
	requireWebhookStatus(t, status, http.StatusOK, accountBody)
	if got := requireWebhookString(t, accountBody, "plan"); got != "pro" {
		t.Fatalf("plan after active subscription webhook = %q, want pro", got)
	}
	if got := requireWebhookString(t, accountBody, "subscription_status"); got != "active" {
		t.Fatalf("subscription_status = %q, want active", got)
	}

	deletedPayload := map[string]any{
		"id":          "evt_test_subscription_deleted",
		"object":      "event",
		"api_version": stripe.APIVersion,
		"type":        "customer.subscription.deleted",
		"data": map[string]any{
			"object": map[string]any{
				"id":       "sub_test_webhook",
				"customer": "cus_test_webhook",
			},
		},
	}
	status, webhookBody = env.signedWebhook(webhookSecret, deletedPayload)
	requireWebhookStatus(t, status, http.StatusOK, webhookBody)

	status, accountBody = env.request(http.MethodGet, "/api/v1/billing/account", apiKey, nil, nil)
	requireWebhookStatus(t, status, http.StatusOK, accountBody)
	if got := requireWebhookString(t, accountBody, "plan"); got != "free" {
		t.Fatalf("plan after deleted subscription webhook = %q, want free", got)
	}
	if got := requireWebhookString(t, accountBody, "subscription_status"); got != "canceled" {
		t.Fatalf("subscription_status = %q, want canceled", got)
	}
}

type webhookTestEnv struct {
	t   *testing.T
	app *fiber.App
}

func newWebhookTestEnv(t *testing.T, webhookSecret string) *webhookTestEnv {
	t.Helper()

	tmp := t.TempDir()
	appDB := openWebhookTestDB(t, filepath.Join(tmp, "app.db"))
	marketDB := openWebhookTestDB(t, filepath.Join(tmp, "market.db"))

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

	repoRoot, err := webhookRepoRoot()
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
		AppName:               "BotTrade Stripe Webhook Test",
		DisableStartupMessage: true,
	})
	apiv1.Mount(app, services.NewScenarioEngine(appDB, marketDB), &config.Config{
		AppBaseURL:          "http://stripe-webhook.example.test",
		StripeWebhookSecret: webhookSecret,
		StripeProPriceID:    "price_test",
	})
	return &webhookTestEnv{t: t, app: app}
}

func openWebhookTestDB(t *testing.T, path string) *sql.DB {
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

func (env *webhookTestEnv) signedWebhook(secret string, payload map[string]any) (int, map[string]any) {
	env.t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		env.t.Fatalf("marshal webhook payload: %v", err)
	}
	signed := stripe.GenerateTestSignedPayload(&stripe.UnsignedPayload{
		Payload: payloadBytes,
		Secret:  secret,
	})
	return env.request(http.MethodPost, "/api/v1/billing/webhook", "", map[string]string{
		"Stripe-Signature": signed.Header,
		"Content-Type":     "application/json",
	}, payload)
}

func (env *webhookTestEnv) request(method, path, key string, headers map[string]string, payload any) (int, map[string]any) {
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
	for k, v := range headers {
		req.Header.Set(k, v)
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
		return resp.StatusCode, map[string]any{"_raw": string(bodyBytes)}
	}
	return resp.StatusCode, decoded
}

func requireWebhookStatus(t *testing.T, got, want int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d; body=%#v", got, want, body)
	}
}

func requireWebhookString(t *testing.T, body map[string]any, key string) string {
	t.Helper()
	v, ok := body[key].(string)
	if !ok || v == "" {
		t.Fatalf("%s missing or not string in %#v", key, body)
	}
	return v
}

func webhookRepoRoot() (string, error) {
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
