package apiv1

import (
	"bottrade/database"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSiteOGImageUsesVersionedRoute(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	MountRunPages(app, "https://bot-trade.org")

	request := httptest.NewRequest(http.MethodGet, "/social-card-20260714.png", nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("GET social card: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", contentType)
	}
	config, err := png.DecodeConfig(response.Body)
	if err != nil {
		t.Fatalf("decode social card: %v", err)
	}
	if config.Width != 1200 || config.Height != 630 {
		t.Fatalf("dimensions = %dx%d, want 1200x630", config.Width, config.Height)
	}
}

func TestRunBadgeSVG(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "badge.db"))
	oldDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = oldDB
		_ = db.Close()
	})

	for _, statement := range []string{
		`CREATE TABLE api_keys (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE scenarios (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE run_results (
			run_id TEXT PRIMARY KEY, return_pct REAL, sharpe REAL,
			max_drawdown REAL, trade_count INTEGER, liquidated INTEGER
		)`,
		`CREATE TABLE run_leaderboard (
			scenario_id TEXT, run_id TEXT, api_key_id TEXT, bot_name TEXT
		)`,
		`INSERT INTO api_keys (id, name) VALUES ('key-1', 'Example')`,
		`INSERT INTO scenarios (id, name) VALUES ('scenario-1', 'Example scenario')`,
		`INSERT INTO run_results
			(run_id, return_pct, sharpe, max_drawdown, trade_count, liquidated)
		 VALUES ('published', 12.34, 1.2, 0.04, 7, 0)`,
		`INSERT INTO run_leaderboard
			(scenario_id, run_id, api_key_id, bot_name)
		 VALUES ('scenario-1', 'published', 'key-1', 'Badge test')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("setup badge database: %v", err)
		}
	}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	MountRunPages(app, "https://bot-trade.org")

	tests := []struct {
		name    string
		value   float64
		message string
		color   string
	}{
		{name: "positive", value: 12.34, message: "+12.34% return", color: "#2e7d32"},
		{name: "neutral", value: 0, message: "+0.00% return", color: "#64748b"},
		{name: "negative", value: -4.5, message: "-4.50% return", color: "#b3261e"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := db.Exec(
				`UPDATE run_results SET return_pct = ?1 WHERE run_id = 'published'`,
				test.value,
			); err != nil {
				t.Fatalf("update return: %v", err)
			}

			response := badgeRequest(t, app, "/run/published/badge.svg")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.StatusCode)
			}
			if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "image/svg+xml") {
				t.Fatalf("content type = %q", contentType)
			}
			if cache := response.Header.Get("Cache-Control"); cache != "public, max-age=300" {
				t.Fatalf("cache control = %q", cache)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read badge: %v", err)
			}
			svg := string(body)
			for _, expected := range []string{
				`role="img"`,
				"tested on BotTrade",
				test.message,
				test.color,
			} {
				if !strings.Contains(svg, expected) {
					t.Fatalf("badge is missing %q: %s", expected, svg)
				}
			}

			second := badgeRequest(t, app, "/run/published/badge.svg")
			secondBody, err := io.ReadAll(second.Body)
			if err != nil {
				t.Fatalf("read second badge: %v", err)
			}
			if string(secondBody) != svg {
				t.Fatal("badge output is not deterministic")
			}
		})
	}

	for _, path := range []string{
		"/run/private/badge.svg",
		"/run/missing/badge.svg",
		"/run/not-a-uuid/badge.svg",
	} {
		response := badgeRequest(t, app, path)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, response.StatusCode)
		}
	}
}

func badgeRequest(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("GET badge: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}
