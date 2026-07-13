package main

import (
	"bottrade/analytics"
	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/jobs"
	"bottrade/services"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/proxy"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	cfg := config.Load()

	// API keys are encrypted at rest with this key. Refuse to boot without it
	// so we never fall back to the insecure dev key in production.
	if cfg.AppEncryptionKey == "" {
		log.Fatal("APP_ENCRYPTION_KEY is required (32-byte hex; generate with `openssl rand -hex 32`)")
	}
	if err := apiv1.InitKeyCipher(cfg.AppEncryptionKey); err != nil {
		log.Fatal("Invalid APP_ENCRYPTION_KEY:", err)
	}

	if err := database.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken); err != nil {
		log.Fatal("Failed to connect to app DB:", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatal("Failed to run app migrations:", err)
	}

	// One-time, idempotent: encrypt any API keys still stored in plaintext.
	if err := apiv1.BackfillAPIKeyEncryption(database.DB); err != nil {
		log.Fatal("Failed to encrypt API keys at rest:", err)
	}

	marketURL := cfg.MarketTursoURL
	if marketURL == "" {
		marketURL = "file:./bottrade-market.db"
		log.Println("Market DB: TURSO_MARKET_DATABASE_URL not set, using ./bottrade-market.db")
	}
	if err := database.ConnectMarket(marketURL, cfg.MarketTursoToken); err != nil {
		log.Fatal("Failed to connect market DB:", err)
	}
	if err := database.RunMigrationsOn(database.MarketDB, "database/migrations_market"); err != nil {
		log.Fatal("Failed to run market migrations:", err)
	}

	analyticsClient := analytics.New(cfg.PostHogAPIKey, cfg.PostHogEndpoint)
	defer analyticsClient.Close()

	engine := services.NewScenarioEngine(database.DB, database.MarketDB)

	scheduler := jobs.NewScheduler()
	scheduler.AddJob(jobs.NewIdleRunCleanupJob())
	scheduler.AddJob(jobs.NewIdempotencySweepJob())
	scheduler.AddJob(jobs.NewRunResultsComputeJob(engine))

	if cfg.AlpacaAPIKey != "" && cfg.AlpacaSecretKey != "" {
		if err := services.InitAlpacaClient(cfg.AlpacaAPIKey, cfg.AlpacaSecretKey, true); err != nil {
			log.Printf("Alpaca: failed to initialize - %v", err)
		} else {
			scheduler.AddJob(jobs.NewBarIngestJob())
		}
	} else {
		log.Println("Alpaca: ALPACA_API_KEY/ALPACA_SECRET_KEY not set, bar ingest disabled")
	}

	scheduler.Start()

	app := fiber.New(fiber.Config{AppName: "BotTrade Benchmark"})
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New())

	apiv1.Mount(app, engine, cfg, analyticsClient)

	mountPostHogProxy(app)

	// Clean-URL aliases for the marketing site. Registered BEFORE app.Static
	// so they win the route match. The .html paths still work via app.Static
	// for backwards compatibility with anything that linked to them.
	mountStaticPageAliases(app)
	apiv1.MountRunPages(app, cfg.AppBaseURL)
	if err := mountArticlePublishing(app, time.Now); err != nil {
		log.Fatal("Failed to mount scheduled articles:", err)
	}
	mountAgentIndex(app)
	mountCrawlDiscoveryAssets(app)
	app.Static("/", "./static")

	log.Printf("Listening on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// mountStaticPageAliases provides extension-free URLs for public site pages.
// Keep these routes before app.Static so the clean paths win route matching.
func mountStaticPageAliases(app *fiber.App) {
	app.Get("/leaderboard", func(c *fiber.Ctx) error { return c.SendFile("./static/leaderboard.html") })
	app.Get("/challenge", func(c *fiber.Ctx) error { return c.SendFile("./static/challenge.html") })
	app.Get("/demo", func(c *fiber.Ctx) error { return c.SendFile("./static/demo.html") })
	app.Get("/scenarios", func(c *fiber.Ctx) error { return c.SendFile("./static/scenarios.html") })
	app.Get("/docs", func(c *fiber.Ctx) error { return c.SendFile("./static/docs.html") })
	app.Get("/methodology", func(c *fiber.Ctx) error { return c.SendFile("./static/methodology.html") })
	app.Get("/pricing", func(c *fiber.Ctx) error { return c.SendFile("./static/pricing.html") })
	app.Get("/contact", func(c *fiber.Ctx) error { return c.SendFile("./static/contact.html") })

	// Editorial resources use one URL namespace. Legacy root-level URLs remain
	// permanent redirects so existing links retain their search value.
	articlePages := map[string]string{
		"ai-trading-bot-backtesting":  "./static/ai-trading-bot-backtesting.html",
		"backtest-ai-trading-agents":  "./static/backtest-ai-trading-agents.html",
		"ai-trading-agent-evaluation": "./static/ai-trading-agent-evaluation.html",
		"mcp-for-trading-agents":      "./static/mcp-for-trading-agents.html",
	}
	for slug, file := range articlePages {
		slug, file := slug, file
		app.Get("/articles/"+slug, func(c *fiber.Ctx) error { return c.SendFile(file) })
		app.Get("/"+slug, func(c *fiber.Ctx) error {
			return c.Redirect("/articles/"+slug, fiber.StatusMovedPermanently)
		})
		app.Get("/"+slug+".html", func(c *fiber.Ctx) error {
			return c.Redirect("/articles/"+slug, fiber.StatusMovedPermanently)
		})
	}
}

// mountCrawlDiscoveryAssets gives search and answer engines stable entry
// points. Keeping these responses revalidatable prevents a cached old 404 from
// hiding a newly published crawl policy or sitemap for hours.
func mountCrawlDiscoveryAssets(app *fiber.App) {
	serveDiscoveryAsset := func(path, contentType string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			body, err := os.ReadFile("./static/" + path)
			if err != nil {
				return err
			}
			c.Set(fiber.HeaderContentType, contentType)
			c.Set(fiber.HeaderCacheControl, "no-cache, max-age=0, must-revalidate")
			return c.Send(body)
		}
	}

	app.Get("/robots.txt", serveDiscoveryAsset("robots.txt", "text/plain; charset=utf-8"))
	app.Get("/sitemap.xml", serveDiscoveryAsset("sitemap.xml", "application/xml; charset=utf-8"))
	app.Get("/llms.txt", serveDiscoveryAsset("llms.txt", "text/plain; charset=utf-8"))
}

// mountPostHogProxy reverse-proxies PostHog through this origin so browser
// analytics survive ad blockers, which block requests to *.posthog.com but not
// to our own domain. The frontend points posthog-js at <origin>/ingest.
//
//   - /ingest/static/*  → us-assets.i.posthog.com  (the JS library + assets)
//   - /ingest/*         → us.i.posthog.com         (event ingestion + flags)
//
// Registered before app.Static so the wildcard wins over the static catch-all.
func mountPostHogProxy(app *fiber.App) {
	const ingestHost = "us.i.posthog.com"
	const assetsHost = "us-assets.i.posthog.com"

	forward := func(c *fiber.Ctx) error {
		rest := c.Params("*")
		host := ingestHost
		if strings.HasPrefix(rest, "static/") {
			host = assetsHost
		}
		target := "https://" + host + "/" + rest
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			target += "?" + string(q)
		}

		// Capture the real visitor IP BEFORE stripping edge headers. Cloudflare
		// puts the true client in CF-Connecting-IP; fall back to the left-most
		// X-Forwarded-For entry, then the socket peer.
		clientIP := strings.TrimSpace(c.Get("CF-Connecting-IP"))
		if clientIP == "" {
			if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
				clientIP = strings.TrimSpace(strings.Split(xff, ",")[0])
			}
		}
		if clientIP == "" {
			clientIP = c.IP()
		}

		// PostHog sits behind Cloudflare and so does this app. Forwarding our
		// edge's Cloudflare/proxy headers makes PostHog's Cloudflare reject the
		// request as a loop ("Error 1000: DNS points to prohibited IP"). Strip
		// those edge headers — and first-party cookies (the bt_session must not
		// leak) — then re-add a clean X-Forwarded-For carrying only the real
		// client IP. PostHog reads the left-most X-Forwarded-For entry for
		// GeoIP, so this keeps visitor locations accurate.
		hdr := &c.Request().Header
		hdr.Del(fiber.HeaderCookie)
		var drop [][]byte
		hdr.VisitAll(func(key, _ []byte) {
			k := strings.ToUpper(string(key))
			if strings.HasPrefix(k, "CF-") ||
				strings.HasPrefix(k, "X-FORWARDED-") ||
				strings.HasPrefix(k, "X-RAILWAY-") ||
				k == "CDN-LOOP" || k == "FORWARDED" || k == "X-REAL-IP" {
				drop = append(drop, append([]byte(nil), key...))
			}
		})
		for _, k := range drop {
			hdr.DelBytes(k)
		}
		hdr.SetHost(host)
		if clientIP != "" {
			hdr.Set(fiber.HeaderXForwardedFor, clientIP)
		}
		return proxy.Do(c, target)
	}

	app.All("/ingest/*", forward)
}
