package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestContactPageAlias(t *testing.T) {
	app := fiber.New()
	mountStaticPageAliases(app)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/contact", nil))
	if err != nil {
		t.Fatalf("GET /contact: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("content type = %q, want HTML", contentType)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read contact page: %v", err)
	}
	page := string(body)
	if !strings.Contains(page, `href="mailto:jyron@bot-trade.org"`) || !strings.Contains(page, `>jyron@bot-trade.org</a>`) {
		t.Fatal("contact page is missing the requested email link")
	}
	if strings.Contains(strings.ToLower(page), "tel:") {
		t.Fatal("contact page must not expose a phone link")
	}

	emails := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`).FindAllString(page, -1)
	for _, email := range emails {
		if email != "jyron@bot-trade.org" {
			t.Fatalf("contact page exposes unexpected email %q", email)
		}
	}
}

func TestGrowthConfigDoesNotExposeCouponConfiguration(t *testing.T) {
	app := fiber.New()
	mountGrowthConfig(app, true)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/site/offers", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != `{"founding_pro":true}` {
		t.Fatalf("unexpected growth config response: status=%d body=%s", response.StatusCode, body)
	}
	if strings.Contains(string(body), "coupon") {
		t.Fatal("growth config must not expose billing configuration")
	}
}

func TestEditorialPagesUseArticlesDirectory(t *testing.T) {
	app := fiber.New()
	mountStaticPageAliases(app)

	pages := map[string][]string{
		"/articles/ai-trading-bot-backtesting":  {"Methodological principles for backtesting AI trading agents", "Exclude future information", "Related BotTrade research"},
		"/articles/backtest-ai-trading-agents":  {"Backtesting autonomous AI trading agents with BotTrade", "historical-market benchmark", `href="/methodology"`},
		"/articles/ai-trading-agent-evaluation": {"Controlled evaluation of AI trading-agent improvements", "identical scenario contract", "publish_run"},
		"/articles/mcp-for-trading-agents":      {"MCP infrastructure for AI trading-agent evaluation", "scan_market", "run_sandbox_smoke_test"},
	}
	for path, fragments := range pages {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Errorf("GET %s: status = %d, want %d", path, response.StatusCode, http.StatusOK)
			continue
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		page := string(body)
		for _, fragment := range fragments {
			if !strings.Contains(page, fragment) {
				t.Errorf("%s is missing %q", path, fragment)
			}
		}
	}

	for _, legacyPath := range []string{
		"/ai-trading-bot-backtesting",
		"/backtest-ai-trading-agents",
		"/ai-trading-agent-evaluation",
		"/mcp-for-trading-agents",
	} {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, legacyPath, nil), -1)
		if err != nil {
			t.Fatalf("GET %s: %v", legacyPath, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusMovedPermanently {
			t.Errorf("GET %s: status = %d, want %d", legacyPath, response.StatusCode, http.StatusMovedPermanently)
		}
		if location := response.Header.Get("Location"); location != "/articles"+legacyPath {
			t.Errorf("GET %s: Location = %q, want %q", legacyPath, location, "/articles"+legacyPath)
		}

		response, err = app.Test(httptest.NewRequest(http.MethodGet, legacyPath+".html", nil), -1)
		if err != nil {
			t.Fatalf("GET %s.html: %v", legacyPath, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusMovedPermanently {
			t.Errorf("GET %s.html: status = %d, want %d", legacyPath, response.StatusCode, http.StatusMovedPermanently)
		}
	}
}

func TestCrawlDiscoveryAssetsAreRevalidatable(t *testing.T) {
	app := fiber.New()
	mountCrawlDiscoveryAssets(app)

	for _, path := range []string{"/robots.txt", "/sitemap.xml", "/llms.txt"} {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status = %d, want %d", path, response.StatusCode, http.StatusOK)
		}
		if cacheControl := response.Header.Get(fiber.HeaderCacheControl); cacheControl != "no-cache, max-age=0, must-revalidate" {
			t.Errorf("GET %s: Cache-Control = %q", path, cacheControl)
		}
	}
}

func TestStaticNavigationIsFocused(t *testing.T) {
	pages := []string{
		"account.html",
		"ai-trading-agent-evaluation.html",
		"ai-trading-bot-backtesting.html",
		"backtest-ai-trading-agents.html",
		"builders.html",
		"challenge.html",
		"contact.html",
		"demo.html",
		"docs.html",
		"index.html",
		"leaderboard.html",
		"mcp-for-trading-agents.html",
		"methodology.html",
		"pricing.html",
		"run.html",
		"scenarios.html",
	}
	for _, page := range pages {
		body, err := os.ReadFile("static/" + page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		navStart := strings.Index(string(body), "<nav")
		navEnd := strings.Index(string(body), "</nav>")
		if navStart < 0 || navEnd < navStart {
			t.Errorf("%s is missing primary navigation", page)
			continue
		}
		nav := string(body)[navStart:navEnd]
		for _, href := range []string{
			`href="/articles"`,
			`href="/ai-trading-agent-index"`,
			`href="/leaderboard"`,
			`href="/challenge"`,
			`href="/scenarios"`,
			`href="/demo"`,
			`href="/articles/ai-trading-bot-backtesting"`,
			`href="/pricing"`,
			`href="/contact"`,
			`href="/docs"`,
			`href="/account"`,
		} {
			if !strings.Contains(nav, href) {
				t.Errorf("%s navigation is missing %s", page, href)
			}
		}
		if details := strings.Count(nav, `<details class="nav-explore">`); details != 1 {
			t.Errorf("%s navigation has %d Explore menus, want 1", page, details)
		}
		if summaries := strings.Count(nav, "<summary"); summaries != 1 {
			t.Errorf("%s navigation has %d visible Explore controls, want 1", page, summaries)
		}
	}
}

func TestPublishedPricingCopy(t *testing.T) {
	for _, page := range []string{"static/pricing.html", "static/account.html"} {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		copy := string(body)
		for _, price := range []string{"$29.99", "$69.99"} {
			if !strings.Contains(copy, price) {
				t.Errorf("%s is missing %s", page, price)
			}
		}
		for _, stale := range []string{"$" + "19.99", "$" + "79.99"} {
			if strings.Contains(copy, stale) {
				t.Errorf("%s still contains stale price %s", page, stale)
			}
		}
	}
}
