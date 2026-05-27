package main

import (
	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/jobs"
	"bottrade/services"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
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

	apiv1.Mount(app, engine, cfg)

	// Clean-URL aliases for the marketing site. Registered BEFORE app.Static
	// so they win the route match. The .html paths still work via app.Static
	// for backwards compatibility with anything that linked to them.
	app.Get("/leaderboard", func(c *fiber.Ctx) error { return c.SendFile("./static/leaderboard.html") })
	app.Get("/scenarios", func(c *fiber.Ctx) error { return c.SendFile("./static/scenarios.html") })
	app.Get("/docs", func(c *fiber.Ctx) error { return c.SendFile("./static/docs.html") })
	app.Get("/methodology", func(c *fiber.Ctx) error { return c.SendFile("./static/methodology.html") })
	app.Get("/pricing", func(c *fiber.Ctx) error { return c.SendFile("./static/pricing.html") })
	app.Get("/billing/success", func(c *fiber.Ctx) error { return c.SendFile("./static/billing-success.html") })
	app.Static("/", "./static")

	log.Printf("Listening on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
