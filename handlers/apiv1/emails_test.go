package apiv1

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bottrade/services"
)

// testMailer returns a Mailer pointed at a local server plus a counter of
// delivered sends.
func testMailer(t *testing.T) (*services.Mailer, *atomic.Int64, *atomic.Value) {
	t.Helper()
	var sends atomic.Int64
	var lastBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("resend auth header = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		lastBody.Store(string(raw))
		sends.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	m := services.NewMailer("test-key", "BotTrade <jyron@bot-trade.org>", "jyron@bot-trade.org")
	m.Endpoint = srv.URL
	return m, &sends, &lastBody
}

func waitForSends(t *testing.T, sends *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sends.Load() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("sends = %d, want %d", sends.Load(), want)
}

func TestMailerSendPayload(t *testing.T) {
	m, sends, lastBody := testMailer(t)
	if err := m.Send("dev@example.test", "subject line", "text body", "<p>html body</p>"); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForSends(t, sends, 1)

	var payload map[string]any
	if err := json.Unmarshal([]byte(lastBody.Load().(string)), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["from"] != "BotTrade <jyron@bot-trade.org>" {
		t.Fatalf("from = %v", payload["from"])
	}
	if payload["subject"] != "subject line" {
		t.Fatalf("subject = %v", payload["subject"])
	}
	if payload["reply_to"] != "jyron@bot-trade.org" {
		t.Fatalf("reply_to = %v", payload["reply_to"])
	}
}

func TestMailerDisabledSkips(t *testing.T) {
	m := services.NewMailer("", "BotTrade <x@y>", "")
	m.Endpoint = "http://127.0.0.1:1" // would fail if contacted
	if err := m.Send("dev@example.test", "s", "t", "h"); err != nil {
		t.Fatalf("disabled mailer should no-op, got %v", err)
	}
}

func TestSendEmailOnceDedupes(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 2.0, true)
	_ = env

	m, sends, _ := testMailer(t)
	h := &handlers{Mailer: m, AppBaseURL: "http://example.test"}

	h.sendEmailOnce("acct-1", "quota_free", "2026-07", "dev@example.test", "s", "t", "h")
	h.sendEmailOnce("acct-1", "quota_free", "2026-07", "dev@example.test", "s", "t", "h") // dup: skipped
	waitForSends(t, sends, 1)

	h.sendEmailOnce("acct-1", "quota_free", "2026-08", "dev@example.test", "s", "t", "h") // new period
	h.sendEmailOnce("acct-1", "quota_pro", "2026-07", "dev@example.test", "s", "t", "h")  // new kind
	waitForSends(t, sends, 3)
}

func TestPlanForSubscription(t *testing.T) {
	h := &handlers{
		StripeMaxPriceID:        "price_max",
		StripeLegacyMaxPriceIDs: []string{"price_max_legacy"},
	}
	cases := []struct {
		status, price, want string
	}{
		{"active", "price_pro", "pro"},
		{"active", "price_max", "max"},
		{"active", "price_max_legacy", "max"},
		{"trialing", "price_max", "max"},
		{"past_due", "price_pro", "pro"},
		{"active", "", "pro"},
		{"canceled", "price_max", "free"},
		{"incomplete_expired", "price_pro", "free"},
	}
	for _, c := range cases {
		if got := h.planForSubscription(c.status, c.price); got != c.want {
			t.Errorf("planForSubscription(%q, %q) = %q, want %q", c.status, c.price, got, c.want)
		}
	}
}

func TestQuotaTiersAndUpgradeEmails(t *testing.T) {
	env := newAPITestEnv(t, defaultAPITestBars(), 100000, 2.0, true)

	m, sends, lastBody := testMailer(t)

	// Free key at limit → 402 with pro upgrade hint + quota_free email.
	keyResp := env.issueTestKey("quota tiers", "tiers@example.test")
	seedMonthlyRuns(t, env.appDB, keyResp.KeyID, env.scenarioID, env.startTime, 25)

	if _, err := env.appDB.Exec(`UPDATE accounts SET email = 'tiers@example.test'`); err != nil {
		t.Fatalf("set email: %v", err)
	}

	// Reach into the mounted handlers is awkward; call enforceRunQuota directly.
	h := &handlers{Mailer: m, AppBaseURL: "http://example.test"}
	key, err := loadAPIKeyByAccountID(keyResp.AccountID)
	if err != nil {
		t.Fatalf("load key: %v", err)
	}

	quotaErr := h.enforceRunQuota(key)
	if quotaErr == nil {
		t.Fatal("free at limit: want error")
	}
	upErr, ok := quotaErr.(*quotaUpgradeError)
	if !ok {
		t.Fatalf("free at limit: got %T", quotaErr)
	}
	if upErr.GetStatus() != http.StatusPaymentRequired || upErr.RunsLimit != 25 {
		t.Fatalf("free at limit: status=%d limit=%d", upErr.GetStatus(), upErr.RunsLimit)
	}
	if !strings.Contains(upErr.UpgradeHint, "plan=pro") {
		t.Fatalf("free hint = %q", upErr.UpgradeHint)
	}
	if !strings.Contains(upErr.UpgradeHint, "$29.99/mo") {
		t.Fatalf("free hint missing Pro price: %q", upErr.UpgradeHint)
	}
	waitForSends(t, sends, 1)
	if body := lastBody.Load().(string); !strings.Contains(body, "25 free runs") || !strings.Contains(body, "$29.99") {
		t.Fatalf("quota_free email body missing pitch or price: %s", body)
	}

	// Pro at limit → 402 with max upgrade hint + quota_pro email.
	if _, err := env.appDB.Exec(`UPDATE accounts SET plan = 'pro'`); err != nil {
		t.Fatalf("set pro: %v", err)
	}
	seedMonthlyRuns(t, env.appDB, keyResp.KeyID, env.scenarioID, env.startTime, 175) // total 200
	key, _ = loadAPIKeyByAccountID(keyResp.AccountID)

	quotaErr = h.enforceRunQuota(key)
	upErr, ok = quotaErr.(*quotaUpgradeError)
	if !ok {
		t.Fatalf("pro at limit: got %T", quotaErr)
	}
	if upErr.GetStatus() != http.StatusPaymentRequired || upErr.RunsLimit != 200 {
		t.Fatalf("pro at limit: status=%d limit=%d", upErr.GetStatus(), upErr.RunsLimit)
	}
	if !strings.Contains(upErr.UpgradeHint, "plan=max") {
		t.Fatalf("pro hint = %q", upErr.UpgradeHint)
	}
	if !strings.Contains(upErr.UpgradeHint, "$69.99/mo") {
		t.Fatalf("pro hint missing Max price: %q", upErr.UpgradeHint)
	}
	waitForSends(t, sends, 2)
	if body := lastBody.Load().(string); !strings.Contains(body, "$69.99") {
		t.Fatalf("quota_pro email body missing max pitch: %s", body)
	}

	// Pro under limit → allowed.
	if _, err := env.appDB.Exec(`DELETE FROM runs`); err != nil {
		t.Fatalf("clear runs: %v", err)
	}
	if quotaErr = h.enforceRunQuota(key); quotaErr != nil {
		t.Fatalf("pro under limit: %v", quotaErr)
	}

	// Max at limit → 429, no upgrade email.
	if _, err := env.appDB.Exec(`UPDATE accounts SET plan = 'max'`); err != nil {
		t.Fatalf("set max: %v", err)
	}
	seedMonthlyRuns(t, env.appDB, keyResp.KeyID, env.scenarioID, env.startTime, 1000)
	key, _ = loadAPIKeyByAccountID(keyResp.AccountID)

	quotaErr = h.enforceRunQuota(key)
	topErr, ok := quotaErr.(*quotaTopLimitError)
	if !ok {
		t.Fatalf("max at limit: got %T", quotaErr)
	}
	if topErr.GetStatus() != http.StatusTooManyRequests || topErr.RunsLimit != 1000 {
		t.Fatalf("max at limit: status=%d limit=%d", topErr.GetStatus(), topErr.RunsLimit)
	}
	time.Sleep(50 * time.Millisecond)
	if sends.Load() != 2 {
		t.Fatalf("max tier should not email, sends = %d", sends.Load())
	}
}
