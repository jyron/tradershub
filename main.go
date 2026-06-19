package main

import (
	"bottrade/analytics"
	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/jobs"
	"bottrade/services"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/proxy"
)

func main() {
	cfg := config.Load()

	if err := database.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken); err != nil {
		log.Fatal("Failed to connect to app DB:", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatal("Failed to run app migrations:", err)
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
	app.Use(logger.New())
	app.Use(cors.New())

	apiv1.Mount(app, engine, cfg, analyticsClient)

	mountPostHogProxy(app)

	// Clean-URL aliases for the marketing site. Registered BEFORE app.Static
	// so they win the route match. The .html paths still work via app.Static
	// for backwards compatibility with anything that linked to them.
	app.Get("/leaderboard", func(c *fiber.Ctx) error { return c.SendFile("./static/leaderboard.html") })
	app.Get("/scenarios", func(c *fiber.Ctx) error { return c.SendFile("./static/scenarios.html") })
	app.Get("/docs", func(c *fiber.Ctx) error { return c.SendFile("./static/docs.html") })
	app.Get("/methodology", func(c *fiber.Ctx) error { return c.SendFile("./static/methodology.html") })
	app.Get("/pricing", func(c *fiber.Ctx) error { return c.SendFile("./static/pricing.html") })
	app.Get("/run/:id", func(c *fiber.Ctx) error { return c.SendFile("./static/run.html") })
	app.Static("/", "./static")

	log.Printf("Listening on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
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
		// Never forward first-party cookies (e.g. the bt_session) to PostHog;
		// posthog-js carries its own state and needs none of ours.
		c.Request().Header.Del(fiber.HeaderCookie)
		c.Request().Header.SetHost(host)
		return proxy.Do(c, target)
	}

	app.All("/ingest/*", forward)
}
