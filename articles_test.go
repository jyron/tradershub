package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestArticleInventoryAndCadence(t *testing.T) {
	library, err := loadArticleLibrary()
	if err != nil {
		t.Fatalf("load article library: %v", err)
	}
	if got, want := len(library.Articles), 36; got != want {
		t.Fatalf("article count = %d, want %d", got, want)
	}

	bogota := time.FixedZone("America/Bogota", -5*60*60)
	byDay := map[string]int{}
	for _, article := range library.Articles {
		day := article.PublishAt.In(bogota).Format("2006-01-02")
		byDay[day]++
	}
	if got, want := len(byDay), 12; got != want {
		t.Fatalf("publication days = %d, want %d", got, want)
	}
	for day, count := range byDay {
		if count != 3 {
			t.Errorf("%s publishes %d articles, want 3", day, count)
		}
	}
}

func TestArticlePublicationGating(t *testing.T) {
	beforeFirst := time.Date(2026, 7, 13, 13, 59, 0, 0, time.UTC)
	app := fiber.New()
	if err := mountArticlePublishing(app, func() time.Time { return beforeFirst }); err != nil {
		t.Fatalf("mount articles: %v", err)
	}

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/articles/claude-vs-gpt-vs-gemini-vs-grok-trading", nil))
	if err != nil {
		t.Fatalf("GET unpublished article: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unpublished status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}

	afterFirst := time.Date(2026, 7, 13, 14, 1, 0, 0, time.UTC)
	app = fiber.New()
	if err := mountArticlePublishing(app, func() time.Time { return afterFirst }); err != nil {
		t.Fatalf("mount articles: %v", err)
	}
	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/articles/claude-vs-gpt-vs-gemini-vs-grok-trading", nil))
	if err != nil {
		t.Fatalf("GET published article: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read published article: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("published status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "Claude vs GPT vs Gemini vs Grok") || !strings.Contains(string(body), "/articles/feed.xml") {
		t.Fatal("published article is missing expected title or feed link")
	}
	const schemaOpen = `<script type="application/ld+json">`
	schemaStart := strings.Index(string(body), schemaOpen)
	schemaEnd := strings.Index(string(body), "</script>")
	if schemaStart < 0 || schemaEnd <= schemaStart {
		t.Fatal("published article is missing Article structured data")
	}
	var schema map[string]any
	if err := json.Unmarshal(body[schemaStart+len(schemaOpen):schemaEnd], &schema); err != nil {
		t.Fatalf("Article structured data is invalid JSON: %v", err)
	}
	if schema["@type"] != "Article" || schema["datePublished"] != "2026-07-13" {
		t.Fatalf("unexpected Article structured data: %#v", schema)
	}
}

func TestScheduledArticlesAreAvailableInNoIndexPreview(t *testing.T) {
	beforeFirst := time.Date(2026, 7, 13, 13, 59, 0, 0, time.UTC)
	app := fiber.New()
	if err := mountArticlePublishing(app, func() time.Time { return beforeFirst }); err != nil {
		t.Fatalf("mount articles: %v", err)
	}

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/articles/preview", nil))
	if err != nil {
		t.Fatalf("GET preview library: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read preview library: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview library status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(response.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatal("preview library is missing its noindex header")
	}
	if !strings.Contains(string(body), "Claude vs GPT vs Gemini vs Grok") || !strings.Contains(string(body), "Twelve Design Principles for High-Performing Autonomous Trading Agents") {
		t.Fatal("preview library does not contain the full scheduled inventory")
	}

	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/articles/preview/claude-vs-gpt-vs-gemini-vs-grok-trading", nil))
	if err != nil {
		t.Fatalf("GET article preview: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("article preview status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(response.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatal("article preview is missing its noindex header")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("article preview Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))
	}
}

func TestArticleDiscoveryOnlyListsPublishedURLs(t *testing.T) {
	now := time.Date(2026, 7, 13, 19, 0, 0, 0, time.UTC)
	app := fiber.New()
	if err := mountArticlePublishing(app, func() time.Time { return now }); err != nil {
		t.Fatalf("mount articles: %v", err)
	}

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/articles/sitemap.xml", nil))
	if err != nil {
		t.Fatalf("GET article sitemap: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read article sitemap: %v", err)
	}
	sitemap := string(body)
	if !strings.Contains(sitemap, "claude-vs-gpt-vs-gemini-vs-grok-trading") || !strings.Contains(sitemap, "best-ai-trading-bots-volatile-markets") {
		t.Fatal("sitemap is missing articles published by the cutoff")
	}
	if strings.Contains(sitemap, "ai-trading-agents-trump-trade-2024") || strings.Contains(sitemap, "best-llms-for-ai-trading-agents-2026") {
		t.Fatal("sitemap exposes an article scheduled after the cutoff")
	}
}
