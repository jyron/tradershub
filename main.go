package main

import (
	"bottrade/config"
	"bottrade/database"
	"bottrade/handlers"
	"bottrade/middleware"
	"bottrade/services"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
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

	app := fiber.New(fiber.Config{
		AppName: "BotTrade v1.0",
	})

	app.Use(logger.New())
	app.Use(cors.New())

	api := app.Group("/api")

	api.Post("/bots/register", handlers.RegisterBot)

	api.Get("/market/quote/:symbol", handlers.GetQuote)
	api.Get("/market/quotes", handlers.GetQuotes)

	api.Post("/trade/stock", middleware.RequireAPIKey, handlers.TradeStock)

	api.Get("/portfolio", middleware.RequireAPIKey, handlers.GetPortfolio)

	app.Static("/", "./static")

	log.Printf("Server starting on port %s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
