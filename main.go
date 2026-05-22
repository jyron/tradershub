package main

import (
	"bottrade/config"
	"bottrade/database"
	"bottrade/handlers"
	apiv1 "bottrade/handlers/apiv1"
	"bottrade/jobs"
	"bottrade/middleware"
	"bottrade/services"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/websocket/v2"
)

func main() {
	cfg := config.Load()

	if err := database.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// API mode also opens the market DB (historical bars + frozen
	// scenario_bars). Site-only deployments skip this so they don't
	// require the second DB to exist. In local dev, default to a file://
	// path so the binary boots without needing a second Turso DB
	// provisioned just to iterate on UI work.
	apiMode := cfg.ServerMode == "api" || cfg.ServerMode == "both"
	if apiMode {
		marketURL := cfg.MarketTursoURL
		if marketURL == "" {
			marketURL = "file:./bottrade-market.db"
			log.Println("⚠️  Market DB: TURSO_MARKET_DATABASE_URL not set, falling back to ./bottrade-market.db")
		}
		if err := database.ConnectMarket(marketURL, cfg.MarketTursoToken); err != nil {
			log.Fatal("Failed to connect market DB:", err)
		}
		if err := database.RunMigrationsOn(database.MarketDB, "database/migrations_market"); err != nil {
			log.Fatal("Failed to run market migrations:", err)
		}
	}

	services.InitMarketData(cfg.MarketAPIKey)
	if cfg.MarketAPIKey == "" || cfg.MarketAPIKey == "your_api_key_here" {
		log.Println("⚠️  Market data: Using MOCK DATA (set MARKET_API_KEY in .env for real data)")
		log.Println("   Get free Finnhub key: https://finnhub.io/register")
	} else {
		log.Println("✓ Market data: Finnhub.io (real-time stock quotes)")
	}

	if err := services.InitKeyVault(cfg.MasterKey, cfg.MasterKeyVersions); err != nil {
		// Submission flow won't work without the master key. Log loudly but
		// don't abort the server — read-only traffic should still serve.
		log.Printf("⚠️  Keyvault: %v — /api/bots/submit will return 503", err)
	} else {
		log.Println("✓ Keyvault: master key loaded (hosted-bot submissions enabled)")
		// Catch removed-version-with-orphaned-rows situations at boot
		// rather than when the first submission tries to decrypt.
		if err := services.VerifyAllCredentialsDecrypt(); err != nil {
			log.Fatalf("✗ Keyvault preflight: %v", err)
		}
	}

	services.SetAnthropicAPIKey(cfg.AnthropicAPIKey)
	if cfg.AnthropicAPIKey == "" {
		log.Println("⚠️  ANTHROPIC_API_KEY not set — daily recap will use template summary")
	} else {
		log.Println("✓ Anthropic: server-side key loaded (daily recap LLM enabled)")
	}

	scheduler := jobs.NewScheduler()
	scheduler.AddJob(jobs.NewPortfolioSnapshotJob())
	scheduler.AddJob(jobs.NewSeasonManagerJob())
	scheduler.AddJob(jobs.NewDailyRecapJob())
	if services.Vault() != nil {
		// Only schedule the hosted-bot runners when the vault is up; without
		// the master key they couldn't decrypt anything anyway.
		scheduler.AddJob(jobs.NewBackfillRunner())
		scheduler.AddJob(jobs.NewDynamicBotRunner())
	}

	if cfg.AlpacaAPIKey != "" && cfg.AlpacaSecretKey != "" {
		if err := services.InitAlpacaClient(cfg.AlpacaAPIKey, cfg.AlpacaSecretKey, cfg.AlpacaPaperMode); err != nil {
			log.Printf("⚠️  Alpaca: Failed to initialize - %v", err)
		} else {
			scheduler.AddJob(jobs.NewAssetSyncJob())
			// Bar ingest pulls hourly bars for the BenchmarkUniverse into
			// market.bars. Only meaningful when the market DB is open
			// (api or both modes).
			if apiMode {
				scheduler.AddJob(jobs.NewBarIngestJob())
			}
		}
	} else {
		log.Println("⚠️  Alpaca: API keys not configured (options trading disabled)")
		log.Println("   Set ALPACA_API_KEY and ALPACA_SECRET_KEY in .env for options trading")
	}

	// Benchmark API maintenance jobs. The results compute job needs the
	// engine; we'll construct that once below for the API listener and
	// the job will receive a pointer to the same instance.
	var benchEngine *services.ScenarioEngine
	if apiMode {
		benchEngine = services.NewScenarioEngine(database.DB, database.MarketDB)
		scheduler.AddJob(jobs.NewIdleRunCleanupJob())
		scheduler.AddJob(jobs.NewIdempotencySweepJob())
		scheduler.AddJob(jobs.NewRunResultsComputeJob(benchEngine))
	}

	scheduler.Start()

	app := fiber.New(fiber.Config{
		AppName: "BotTrade v1.0",
	})

	app.Use(logger.New())
	app.Use(cors.New())

	// Rate limiter for bot registration - 5 registrations per hour per IP
	registrationLimiter := limiter.New(limiter.Config{
		Max:        5,
		Expiration: 1 * time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many registration attempts. Please try again later.",
			})
		},
	})

	// Rate limiter for bot claiming - 10 claims per hour per IP
	claimLimiter := limiter.New(limiter.Config{
		Max:        10,
		Expiration: 1 * time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many claim attempts. Please try again later.",
			})
		},
	})

	// Rate limiter for hosted-bot submissions - 3 per hour per IP.
	// Tighter than register because each submission spawns a python child.
	submissionLimiter := limiter.New(limiter.Config{
		Max:        3,
		Expiration: 1 * time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many submission attempts. Please try again later.",
			})
		},
	})

	api := app.Group("/api")

	api.Post("/bots/register", registrationLimiter, handlers.RegisterBot)
	api.Post("/bots/submit", submissionLimiter, handlers.SubmitBot)
	api.Get("/bots/:bot_id", handlers.GetBotDetails)
	api.Get("/bots/:bot_id/backfill", handlers.GetBotBackfill)
	api.Get("/backfill/:job_id", handlers.GetBackfillJob)
	api.Post("/claim/:bot_id", claimLimiter, handlers.ClaimBot)

	api.Get("/market/quote/:symbol", handlers.GetQuote)
	api.Get("/market/quotes", handlers.GetQuotes)
	api.Get("/market/history/:symbol", handlers.GetHistoricalCandles)

	api.Post("/trade/stock", middleware.RequireAPIKey, handlers.TradeStock)
	api.Post("/trade/option", middleware.RequireAPIKey, handlers.TradeOption)

	api.Get("/options/chain/:symbol", handlers.GetOptionChain)

	api.Get("/assets", handlers.GetAssets)

	api.Get("/portfolio", middleware.RequireAPIKey, handlers.GetPortfolio)

	api.Get("/leaderboard", handlers.GetLeaderboard)
	api.Get("/stats", handlers.GetStats)
	api.Get("/vs", handlers.Vs)

	api.Get("/seasons", handlers.GetSeasons)
	api.Get("/seasons/:id", handlers.GetSeason)
	api.Get("/seasons/:id/leaderboard", handlers.GetSeasonLeaderboard)
	api.Post("/seasons/:id/enroll", middleware.RequireAPIKey, handlers.EnrollInSeason)
	api.Post("/admin/seasons", handlers.RequireAdminSecret, handlers.CreateSeason)
	api.Post("/admin/seasons/:id/start", handlers.RequireAdminSecret, handlers.ForceStartSeason)
	api.Post("/admin/seasons/:id/close", handlers.RequireAdminSecret, handlers.ForceCloseSeason)
	api.Post("/admin/bots/:id/tier", handlers.RequireAdminSecret, handlers.PromoteBotTier)

	api.Get("/methodology", handlers.GetMethodology)
	api.Get("/methodology/prompt", handlers.GetMethodologyPrompt)

	api.Get("/recaps", handlers.ListRecaps)
	api.Get("/recap/:date", handlers.GetRecap)

	// OG image cards. Twitter/Discord/Slack pull these into link previews.
	// Filename suffix ".png" is accepted so crawlers that path-sniff format
	// see what they expect.
	app.Get("/og/bot/:id", handlers.GetOGBot)
	app.Get("/og/leaderboard", handlers.GetOGLeaderboard)
	app.Get("/og/leaderboard.png", handlers.GetOGLeaderboard)
	app.Get("/og/trade/:id", handlers.GetOGTrade)

	// Server-rendered shell for /bots.html that injects per-bot og: meta.
	// Must be registered BEFORE app.Static so we win the route match.
	app.Get("/bots.html", handlers.BotPageMeta)

	// Clean-URL aliases for Phase 4 IA. Keep the .html paths working so
	// existing OG-cached share links don't break.
	app.Get("/models", func(c *fiber.Ctx) error { return c.SendFile("./static/models.html") })
	app.Get("/methodology", func(c *fiber.Ctx) error { return c.SendFile("./static/methodology.html") })
	app.Get("/submit", func(c *fiber.Ctx) error { return c.SendFile("./static/submit.html") })
	app.Get("/today", func(c *fiber.Ctx) error { return c.SendFile("./static/today.html") })
	app.Get("/leaderboard", func(c *fiber.Ctx) error { return c.SendFile("./static/leaderboard.html") })
	app.Get("/feed", func(c *fiber.Ctx) error { return c.SendFile("./static/feed.html") })

	// Embeddable widget: allows iframe from any third-party site. Headers
	// stripped from X-Frame-Options: DENY defaults.
	app.Use("/embed", handlers.EmbedHeaders)

	// RSS feeds — global trades, per-bot trades, daily recaps.
	app.Get("/rss/trades.xml", handlers.RSSGlobalTrades)
	app.Get("/rss/bot/:id", handlers.RSSBotTrades)
	app.Get("/rss/recaps.xml", handlers.RSSRecaps)

	// WebSocket endpoint
	app.Use("/ws", handlers.WebSocketUpgrade)
	app.Get("/ws", websocket.New(handlers.WebSocketHandler))

	app.Static("/", "./static")

	// Mount the Benchmark API (/v1/*) on its own Fiber app + port. Lives
	// in the same process for MVP but is fully independent: separate
	// listener, separate routing tree, doesn't share middleware or
	// connection-pool contention with /api/*.
	//
	// In mode=api: only the API listens (foreground).
	// In mode=both: API listens in a goroutine, site is foreground.
	// In mode=site: API is skipped entirely.
	if apiMode {
		// Reuse the same engine the results-compute job is wired to so
		// there's a single per-process locks map.
		engine := benchEngine
		apiApp := fiber.New(fiber.Config{AppName: "BotTrade API v1"})
		apiApp.Use(logger.New())
		apiApp.Use(cors.New())

		// Friendly root + healthcheck so curling the bare service URL
		// doesn't look like a misconfiguration.
		apiApp.Get("/", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{
				"name":    "BotTrade Benchmark API",
				"version": "v1",
				"docs":    "https://bot-trade.org/methodology",
				"endpoints": []string{
					"GET    /v1/scenarios",
					"GET    /v1/scenarios/:id",
					"POST   /v1/runs",
					"GET    /v1/runs/:id",
					"GET    /v1/runs/:id/market",
					"POST   /v1/runs/:id/trades",
					"POST   /v1/runs/:id/step",
					"GET    /v1/runs/:id/results",
					"POST   /v1/runs/:id/publish",
				},
				"auth": "Send X-API-Key header on all /v1/* requests.",
			})
		})
		apiApp.Get("/health", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{"ok": true})
		})

		apiv1.Mount(apiApp, engine)

		// In api-only mode this is the only listener, so we listen on
		// $PORT (what Railway/Heroku-style platforms assign and proxy to).
		// In mode=both the site already owns $PORT, so the API uses the
		// dedicated $API_PORT — set to a port the platform isn't routing
		// public traffic to.
		apiListenPort := cfg.APIPort
		if cfg.ServerMode == "api" {
			apiListenPort = cfg.Port
		}
		log.Printf("Benchmark API starting on port %s", apiListenPort)
		if cfg.ServerMode == "api" {
			if err := apiApp.Listen(":" + apiListenPort); err != nil {
				log.Fatal("Failed to start API:", err)
			}
			return
		}
		// mode=both: API in background; site below blocks
		go func() {
			if err := apiApp.Listen(":" + apiListenPort); err != nil {
				log.Printf("API listener exited: %v", err)
			}
		}()
	}

	log.Printf("Server starting on port %s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
