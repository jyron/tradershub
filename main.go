package main

import (
	"bottrade/config"
	"bottrade/database"
	"bottrade/handlers"
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

	if err := database.Connect(cfg.DatabaseURL); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	services.InitMarketData(cfg.MarketAPIKey)
	if cfg.MarketAPIKey == "" || cfg.MarketAPIKey == "your_api_key_here" {
		log.Println("⚠️  Market data: Using MOCK DATA (set MARKET_API_KEY in .env for real data)")
		log.Println("   Get free Finnhub key: https://finnhub.io/register")
	} else {
		log.Println("✓ Market data: Finnhub.io (real-time stock quotes)")
	}

	if cfg.AlpacaAPIKey != "" && cfg.AlpacaSecretKey != "" {
		if err := services.InitAlpacaClient(cfg.AlpacaAPIKey, cfg.AlpacaSecretKey, cfg.AlpacaPaperMode); err != nil {
			log.Printf("⚠️  Alpaca: Failed to initialize - %v", err)
		} else {
			scheduler := jobs.NewScheduler()
			scheduler.AddJob(jobs.NewAssetSyncJob())
			scheduler.Start()
		}
	} else {
		log.Println("⚠️  Alpaca: API keys not configured (options trading disabled)")
		log.Println("   Set ALPACA_API_KEY and ALPACA_SECRET_KEY in .env for options trading")
	}

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

	api := app.Group("/api")

	api.Post("/bots/register", registrationLimiter, handlers.RegisterBot)
	api.Get("/bots/:bot_id", handlers.GetBotDetails)
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

	// WebSocket endpoint
	app.Use("/ws", handlers.WebSocketUpgrade)
	app.Get("/ws", websocket.New(handlers.WebSocketHandler))

	app.Static("/", "./static")

	log.Printf("Server starting on port %s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
