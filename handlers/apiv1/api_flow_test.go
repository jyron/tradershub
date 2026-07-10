package apiv1

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"bottrade/config"
	"bottrade/database"
	"bottrade/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v82"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type apiTestEnv struct {
	t            *testing.T
	app          *fiber.App
	appDB        *sql.DB
	marketDB     *sql.DB
	scenarioID   string
	scenarioSlug string
	startTime    time.Time
}

type apiTestBar struct {
	symbol                 string
	open, high, low, close float64
	volume                 int64
}

func TestAPIUserRunLifecycle(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 3.0, true)

	status, body := env.request(http.MethodPost, "/api/v1/keys", "", map[string]any{
		"name": "anonymous agent",
	})
	requireStatus(t, status, http.StatusUnauthorized, body)

	keyResp := env.issueTestKey(" api flow test ", "api-flow@example.test")
	key := keyResp.APIKey
	if got := keyResp.Plan; got != "free" {
		t.Fatalf("new key plan = %q, want free", got)
	}

	status, scenarios := env.request(http.MethodGet, "/api/v1/scenarios", "", nil)
	requireStatus(t, status, http.StatusOK, scenarios)
	if len(requireSlice(t, scenarios, "scenarios")) != 1 {
		t.Fatalf("expected one ready scenario, got %#v", scenarios["scenarios"])
	}

	status, scenario := env.request(http.MethodGet, "/api/v1/scenarios/"+env.scenarioSlug, "", nil)
	requireStatus(t, status, http.StatusOK, scenario)

	status, missingAuth := env.request(http.MethodPost, "/api/v1/runs", "", map[string]any{
		"scenario_slug": env.scenarioSlug,
	})
	requireStatus(t, status, http.StatusUnauthorized, missingAuth)

	status, runBody := env.request(http.MethodPost, "/api/v1/runs", key, map[string]any{
		"scenario_slug": env.scenarioSlug,
		"bot_name":      "flow-bot",
	})
	requireStatus(t, status, http.StatusCreated, runBody)
	run := requireMap(t, runBody, "run")
	runID := requireString(t, run, "id")
	if got := requireString(t, run, "status"); got != "active" {
		t.Fatalf("run status = %q, want active", got)
	}

	secondKey := env.issueTestKey("other user", "").APIKey
	status, forbidden := env.request(http.MethodGet, "/api/v1/runs/"+runID, secondKey, nil)
	requireStatus(t, status, http.StatusForbidden, forbidden)

	status, market := env.request(http.MethodGet, "/api/v1/runs/"+runID+"/market?symbols=AAPL,MSFT&lookback=1", key, nil)
	requireStatus(t, status, http.StatusOK, market)
	bars := requireMap(t, market, "bars")
	if len(requireSlice(t, bars, "AAPL")) != 1 || len(requireSlice(t, bars, "MSFT")) != 1 {
		t.Fatalf("market lookback did not return one bar per requested symbol: %#v", bars)
	}

	status, badTrade := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, map[string]any{
		"symbol": "TSLA", "side": "buy", "quantity": 1,
	})
	requireStatus(t, status, http.StatusBadRequest, badTrade)

	status, badSell := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, map[string]any{
		"symbol": "AAPL", "side": "sell", "quantity": 1,
	})
	requireStatus(t, status, http.StatusBadRequest, badSell)

	tradeReq := map[string]any{
		"symbol": "AAPL", "side": "buy", "quantity": 10,
		"reasoning": "first buy", "idempotency_key": "trade-buy-aapl",
	}
	status, trade1 := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, tradeReq)
	requireStatus(t, status, http.StatusCreated, trade1)
	status, trade2 := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, tradeReq)
	requireStatus(t, status, http.StatusCreated, trade2)
	requireJSONEqual(t, trade1, trade2)

	reusedKey := map[string]any{
		"symbol": "AAPL", "side": "buy", "quantity": 11,
		"reasoning": "changed body", "idempotency_key": "trade-buy-aapl",
	}
	status, conflict := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, reusedKey)
	requireStatus(t, status, http.StatusConflict, conflict)

	status, activeResults := env.request(http.MethodGet, "/api/v1/runs/"+runID+"/results", key, nil)
	requireStatus(t, status, http.StatusBadRequest, activeResults)

	status, step1 := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/step", key, map[string]any{
		"idempotency_key": "step-buy-fill",
	})
	requireStatus(t, status, http.StatusOK, step1)
	if fills := requireSlice(t, step1, "fills"); len(fills) != 1 {
		t.Fatalf("step fills = %d, want 1", len(fills))
	}

	status, snapshot := env.request(http.MethodGet, "/api/v1/runs/"+runID, key, nil)
	requireStatus(t, status, http.StatusOK, snapshot)
	requirePosition(t, snapshot, "AAPL", 10)

	queueAndStep(t, env, key, runID, "sell-aapl", "AAPL", "sell", 5)
	requirePositionAfterGet(t, env, key, runID, "AAPL", 5)
	queueAndStep(t, env, key, runID, "short-msft", "MSFT", "short", 3)
	requirePositionAfterGet(t, env, key, runID, "MSFT", -3)
	queueAndStep(t, env, key, runID, "cover-msft", "MSFT", "cover", 1)
	requirePositionAfterGet(t, env, key, runID, "MSFT", -2)

	status, doneStep := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/step", key, map[string]any{
		"count": 1000, "idempotency_key": "step-complete",
	})
	requireStatus(t, status, http.StatusOK, doneStep)
	if done, _ := doneStep["done"].(bool); !done {
		t.Fatalf("final step done = %v, want true", doneStep["done"])
	}

	status, resultsBody := env.request(http.MethodGet, "/api/v1/runs/"+runID+"/results", key, nil)
	requireStatus(t, status, http.StatusOK, resultsBody)
	results := requireMap(t, resultsBody, "results")
	returnPct := requireFloat(t, results, "return_pct")
	if math.IsNaN(returnPct) || math.IsInf(returnPct, 0) {
		t.Fatalf("return_pct is not finite: %v", returnPct)
	}
	if tradeCount := int(requireFloat(t, results, "trade_count")); tradeCount != 4 {
		t.Fatalf("trade_count = %d, want 4", tradeCount)
	}

	status, publishBody := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/publish", key, nil)
	requireStatus(t, status, http.StatusOK, publishBody)
	if published, _ := publishBody["published"].(bool); !published {
		t.Fatalf("publish response published = %v, want true", publishBody["published"])
	}

	status, publicRun := env.request(http.MethodGet, "/api/v1/runs/"+runID+"/public", "", nil)
	requireStatus(t, status, http.StatusOK, publicRun)

	status, stepAfterDone := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/step", key, map[string]any{"count": 1})
	requireStatus(t, status, http.StatusBadRequest, stepAfterDone)
}

func TestAPIRunLoopLiquidation(t *testing.T) {
	bars := [][]apiTestBar{
		{{symbol: "AAPL", open: 100, high: 101, low: 99, close: 100, volume: 1000}},
		{{symbol: "AAPL", open: 100, high: 100, low: 8, close: 10, volume: 1000}},
		{{symbol: "AAPL", open: 10, high: 12, low: 9, close: 11, volume: 1000}},
	}
	env := newAPITestEnv(t, bars, 1000, 4.0, false)

	key := env.issueTestKey("liquidation", "").APIKey

	status, runBody := env.request(http.MethodPost, "/api/v1/runs", key, map[string]any{"scenario_slug": env.scenarioSlug})
	requireStatus(t, status, http.StatusCreated, runBody)
	runID := requireString(t, requireMap(t, runBody, "run"), "id")

	status, tradeBody := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, map[string]any{
		"symbol": "AAPL", "side": "buy", "quantity": 35,
	})
	requireStatus(t, status, http.StatusCreated, tradeBody)

	status, stepBody := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/step", key, map[string]any{"count": 1})
	requireStatus(t, status, http.StatusOK, stepBody)
	if liquidated, _ := stepBody["liquidated"].(bool); !liquidated {
		t.Fatalf("liquidated = %v, want true; body=%#v", stepBody["liquidated"], stepBody)
	}

	status, snapshot := env.request(http.MethodGet, "/api/v1/runs/"+runID, key, nil)
	requireStatus(t, status, http.StatusOK, snapshot)
	if got := requireString(t, requireMap(t, snapshot, "run"), "status"); got != "liquidated" {
		t.Fatalf("run status = %q, want liquidated", got)
	}
	positions, ok := snapshot["positions"].([]any)
	if !ok && snapshot["positions"] != nil {
		t.Fatalf("positions missing or not array/null in %#v", snapshot)
	}
	if len(positions) != 0 {
		t.Fatalf("positions after liquidation = %#v, want none", positions)
	}

	status, resultsBody := env.request(http.MethodGet, "/api/v1/runs/"+runID+"/results", key, nil)
	requireStatus(t, status, http.StatusOK, resultsBody)
	results := requireMap(t, resultsBody, "results")
	if liquidated, _ := results["liquidated"].(bool); !liquidated {
		t.Fatalf("results liquidated = %v, want true", results["liquidated"])
	}
}

func TestAPIProUpgradeUnlocksFreeQuota(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 2.0, true)

	keyResp := env.issueTestKey("quota upgrade", "")
	key := keyResp.APIKey
	keyID := keyResp.KeyID

	seedMonthlyRuns(t, env.appDB, keyID, env.scenarioID, env.startTime, 25)

	status, quotaBody := env.request(http.MethodPost, "/api/v1/runs", key, map[string]any{
		"scenario_slug": env.scenarioSlug,
	})
	requireStatus(t, status, http.StatusPaymentRequired, quotaBody)

	cs := &stripe.CheckoutSession{
		Metadata:      map[string]string{"api_key_id": keyID},
		PaymentStatus: stripe.CheckoutSessionPaymentStatusPaid,
	}
	if err := (&handlers{}).applyCheckoutSession(cs); err != nil {
		t.Fatalf("apply checkout session: %v", err)
	}

	status, accountBody := env.request(http.MethodGet, "/api/v1/billing/account", key, nil)
	requireStatus(t, status, http.StatusOK, accountBody)
	if got := requireString(t, accountBody, "plan"); got != "pro" {
		t.Fatalf("billing account plan = %q, want pro", got)
	}

	status, handleBody := env.request(http.MethodPatch, "/api/v1/billing/account", key, map[string]any{
		"handle": "quota-pro",
	})
	requireStatus(t, status, http.StatusOK, handleBody)
	if got := requireString(t, handleBody, "handle"); got != "quota-pro" {
		t.Fatalf("handle = %q, want quota-pro", got)
	}

	status, runBody := env.request(http.MethodPost, "/api/v1/runs", key, map[string]any{
		"scenario_slug": env.scenarioSlug,
	})
	requireStatus(t, status, http.StatusCreated, runBody)
}

func TestAPIAcceptsBearerAPIKey(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 2.0, true)

	key := env.issueTestKey("bearer auth", "").APIKey

	status, scenarios := env.requestWithBearer(http.MethodGet, "/api/v1/billing/account", key, nil)
	requireStatus(t, status, http.StatusOK, scenarios)
	if got := requireString(t, scenarios, "account_id"); got == "" {
		t.Fatalf("account_id empty")
	}
}

func TestAPIAcceptsOAuthBearerToken(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 2.0, true)

	keyResp := env.issueTestKey("oauth bearer", "")
	accountID := keyResp.AccountID
	token := "bt_oat_test"
	_, err := env.appDB.Exec(
		`INSERT INTO oauth_clients (id, name, redirect_uris)
		 VALUES ('test-client', 'Test Client', '["https://client.example/callback"]')`,
	)
	if err != nil {
		t.Fatalf("insert oauth client: %v", err)
	}
	_, err = env.appDB.Exec(
		`INSERT INTO oauth_access_tokens
		   (token_hash, account_id, client_id, scope, resource, expires_at)
		 VALUES (?1, ?2, 'test-client', 'bottrade:trade', 'https://mcp.bot-trade.org/mcp', ?3)`,
		hashToken(token), accountID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert oauth token: %v", err)
	}

	// A bottrade:trade OAuth token authenticates on trading endpoints.
	status, runBody := env.requestWithBearer(http.MethodPost, "/api/v1/runs", token, map[string]any{
		"scenario_slug": env.scenarioSlug,
		"bot_name":      "oauth-bot",
	})
	requireStatus(t, status, http.StatusCreated, runBody)

	// ...but is scoped out of account/billing management (phished/over-broad
	// tokens can never touch billing).
	status, denied := env.requestWithBearer(http.MethodGet, "/api/v1/billing/account", token, nil)
	requireStatus(t, status, http.StatusForbidden, denied)
}

func newAPITestEnv(t *testing.T, bars [][]apiTestBar, startingCash, leverageCap float64, shortEnabled bool) *apiTestEnv {
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

	repoRoot, err := apiTestRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if err := database.RunMigrationsOn(appDB, filepath.Join(repoRoot, "database/migrations")); err != nil {
		t.Fatalf("app migrations: %v", err)
	}
	if err := database.RunMigrationsOn(marketDB, filepath.Join(repoRoot, "database/migrations_market")); err != nil {
		t.Fatalf("market migrations: %v", err)
	}

	env := &apiTestEnv{
		t:            t,
		appDB:        appDB,
		marketDB:     marketDB,
		scenarioID:   uuid.NewString(),
		scenarioSlug: "api-flow-" + uuid.NewString()[:8],
		startTime:    time.Date(2024, 6, 3, 13, 0, 0, 0, time.UTC),
	}
	env.insertScenario(t, bars, startingCash, leverageCap, shortEnabled)

	engine := services.NewScenarioEngine(appDB, marketDB)
	env.app = fiber.New(fiber.Config{
		AppName:               "BotTrade API Test",
		DisableStartupMessage: true,
	})
	Mount(env.app, engine, &config.Config{
		AppBaseURL:       "http://example.test",
		StripeProPriceID: "price_test",
	}, nil)
	return env
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

func (env *apiTestEnv) insertScenario(t *testing.T, bars [][]apiTestBar, startingCash, leverageCap float64, shortEnabled bool) {
	t.Helper()
	if len(bars) == 0 || len(bars[0]) == 0 {
		t.Fatal("test scenario needs at least one bar")
	}

	universeSet := map[string]struct{}{}
	for _, hourBars := range bars {
		for _, bar := range hourBars {
			universeSet[bar.symbol] = struct{}{}
		}
	}
	universe := make([]string, 0, len(universeSet))
	slippage := map[string]int{}
	for symbol := range universeSet {
		universe = append(universe, symbol)
		slippage[symbol] = 0
	}
	sort.Strings(universe)
	universeJSON, _ := json.Marshal(universe)
	slippageJSON, _ := json.Marshal(slippage)

	shortInt := 0
	if shortEnabled {
		shortInt = 1
	}
	endTime := env.startTime.Add(time.Duration(len(bars)-1) * time.Hour)
	if _, err := env.appDB.Exec(`
		INSERT INTO scenarios (
			id, slug, name, description, bar_resolution, start_ts, end_ts,
			starting_cash, leverage_cap, short_enabled, universe_json,
			slippage_json, benchmark_symbol, status, current_version
		) VALUES (
			?1, ?2, 'API Flow Scenario', 'synthetic integration-test scenario',
			'1Hour', ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, 'ready', 1
		)
	`, env.scenarioID, env.scenarioSlug, env.startTime.Format(time.RFC3339),
		endTime.Format(time.RFC3339), startingCash, leverageCap, shortInt,
		string(universeJSON), string(slippageJSON), universe[0]); err != nil {
		t.Fatalf("insert scenario: %v", err)
	}
	if _, err := env.appDB.Exec(`
		INSERT INTO scenario_versions (scenario_id, version, bars_captured_at, bar_count)
		VALUES (?1, 1, ?2, ?3)
	`, env.scenarioID, time.Now().UTC().Format(time.RFC3339), len(bars)); err != nil {
		t.Fatalf("insert scenario version: %v", err)
	}

	for i, hourBars := range bars {
		ts := env.startTime.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		for _, bar := range hourBars {
			if _, err := env.marketDB.Exec(`
				INSERT INTO scenario_bars (
					scenario_id, scenario_version, symbol, ts, open, high, low,
					close, volume, slippage_bps
				) VALUES (?1, 1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, 0)
			`, env.scenarioID, bar.symbol, ts, bar.open, bar.high, bar.low,
				bar.close, bar.volume); err != nil {
				t.Fatalf("insert scenario bar %d %s: %v", i, bar.symbol, err)
			}
		}
	}
}

func defaultAPITestBars() [][]apiTestBar {
	out := make([][]apiTestBar, 8)
	for i := range out {
		aapl := 100.0 + float64(i*2)
		msft := 50.0 - float64(i)
		out[i] = []apiTestBar{
			{symbol: "AAPL", open: aapl, high: aapl + 1, low: aapl - 1, close: aapl + 0.5, volume: 1000},
			{symbol: "MSFT", open: msft, high: msft + 1, low: msft - 1, close: msft - 0.25, volume: 1000},
		}
	}
	return out
}

func (env *apiTestEnv) request(method, path, key string, payload any) (int, map[string]any) {
	env.t.Helper()
	return env.requestWithHeaders(method, path, map[string]string{"X-API-Key": key}, payload)
}

func (env *apiTestEnv) requestWithBearer(method, path, key string, payload any) (int, map[string]any) {
	env.t.Helper()
	return env.requestWithHeaders(method, path, map[string]string{"Authorization": "Bearer " + key}, payload)
}

func (env *apiTestEnv) issueTestKey(name, email string) issueKeyResponse {
	env.t.Helper()
	resp, err := createAPIKey(name, email, "free")
	if err != nil {
		env.t.Fatalf("create test API key: %v", err)
	}
	return resp
}

func (env *apiTestEnv) requestWithHeaders(method, path string, headers map[string]string, payload any) (int, map[string]any) {
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
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
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

func queueAndStep(t *testing.T, env *apiTestEnv, key, runID, idempotencyKey, symbol, side string, qty int) {
	t.Helper()
	status, tradeBody := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/trades", key, map[string]any{
		"symbol": symbol, "side": side, "quantity": qty, "idempotency_key": "trade-" + idempotencyKey,
	})
	requireStatus(t, status, http.StatusCreated, tradeBody)
	status, stepBody := env.request(http.MethodPost, "/api/v1/runs/"+runID+"/step", key, map[string]any{
		"count": 1, "idempotency_key": "step-" + idempotencyKey,
	})
	requireStatus(t, status, http.StatusOK, stepBody)
}

func seedMonthlyRuns(t *testing.T, db *sql.DB, keyID, scenarioID string, simTime time.Time, count int) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < count; i++ {
		if _, err := db.Exec(`
			INSERT INTO runs (
				id, api_key_id, bot_name, scenario_id, scenario_version, status,
				sim_time, cash, starting_cash, last_activity_at, created_at,
				completed_at, published
			) VALUES (?1, ?2, 'quota-seed', ?3, 1, 'completed', ?4, 100000,
				100000, ?5, ?5, ?5, 0)
		`, uuid.NewString(), keyID, scenarioID, simTime.Format(time.RFC3339), now); err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
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

func requireFloat(t *testing.T, body map[string]any, key string) float64 {
	t.Helper()
	v, ok := body[key].(float64)
	if !ok {
		t.Fatalf("%s missing or not number in %#v", key, body)
	}
	return v
}

func requireMap(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := body[key].(map[string]any)
	if !ok {
		t.Fatalf("%s missing or not object in %#v", key, body)
	}
	return v
}

func requireSlice(t *testing.T, body map[string]any, key string) []any {
	t.Helper()
	v, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s missing or not array in %#v", key, body)
	}
	return v
}

func requireJSONEqual(t *testing.T, a, b map[string]any) {
	t.Helper()
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if !bytes.Equal(aj, bj) {
		t.Fatalf("JSON differs:\nfirst:  %s\nsecond: %s", aj, bj)
	}
}

func requirePositionAfterGet(t *testing.T, env *apiTestEnv, key, runID, symbol string, quantity int) {
	t.Helper()
	status, snapshot := env.request(http.MethodGet, "/api/v1/runs/"+runID, key, nil)
	requireStatus(t, status, http.StatusOK, snapshot)
	requirePosition(t, snapshot, symbol, quantity)
}

func requirePosition(t *testing.T, snapshot map[string]any, symbol string, quantity int) {
	t.Helper()
	for _, raw := range requireSlice(t, snapshot, "positions") {
		pos, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("position is not object: %#v", raw)
		}
		if pos["symbol"] == symbol {
			got := int(requireFloat(t, pos, "quantity"))
			if got != quantity {
				t.Fatalf("position %s quantity = %d, want %d", symbol, got, quantity)
			}
			return
		}
	}
	t.Fatalf("position %s not found in %#v", symbol, snapshot["positions"])
}

func apiTestRepoRoot() (string, error) {
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
