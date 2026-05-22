// backfill_bars pulls historical hourly bars from Alpaca for every symbol in
// services.BenchmarkUniverse over a date range and writes them into the
// market DB's `bars` table. Idempotent — re-runs are safe.
//
// Usage:
//   go run ./cmd/backfill_bars --start 2024-01-01 --end 2024-06-30
//   go run ./cmd/backfill_bars --start 2024-01-01 --end 2024-06-30 --symbols AAPL,MSFT
//
// Honors TURSO_MARKET_DATABASE_URL / TURSO_MARKET_AUTH_TOKEN from .env. Falls
// back to a local file (./bottrade-market.db) if unset.
package main

import (
	"bottrade/config"
	"bottrade/database"
	"bottrade/jobs"
	"bottrade/services"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	var (
		startStr   = flag.String("start", "", "start date YYYY-MM-DD (required)")
		endStr     = flag.String("end", "", "end date YYYY-MM-DD (required)")
		symbolsStr = flag.String("symbols", "", "comma-separated symbols (default: full BenchmarkUniverse)")
		chunkDays  = flag.Int("chunk-days", 30, "split the range into chunks of this many days per Alpaca request")
	)
	flag.Parse()

	if *startStr == "" || *endStr == "" {
		fmt.Fprintln(os.Stderr, "usage: backfill_bars --start YYYY-MM-DD --end YYYY-MM-DD [--symbols A,B] [--chunk-days N]")
		os.Exit(2)
	}

	start, err := time.Parse("2006-01-02", *startStr)
	if err != nil {
		log.Fatalf("invalid --start: %v", err)
	}
	end, err := time.Parse("2006-01-02", *endStr)
	if err != nil {
		log.Fatalf("invalid --end: %v", err)
	}
	if !end.After(start) {
		log.Fatal("--end must be after --start")
	}

	symbols := services.BenchmarkUniverse
	if *symbolsStr != "" {
		symbols = strings.Split(*symbolsStr, ",")
		for i := range symbols {
			symbols[i] = strings.TrimSpace(symbols[i])
		}
	}

	cfg := config.Load()
	if cfg.AlpacaAPIKey == "" || cfg.AlpacaSecretKey == "" {
		log.Fatal("ALPACA_API_KEY and ALPACA_SECRET_KEY must be set")
	}
	if err := services.InitAlpacaClient(cfg.AlpacaAPIKey, cfg.AlpacaSecretKey, cfg.AlpacaPaperMode); err != nil {
		log.Fatalf("init alpaca: %v", err)
	}

	marketURL := cfg.MarketTursoURL
	if marketURL == "" {
		marketURL = "file:./bottrade-market.db"
		log.Printf("Market DB URL not set, using local file %s", marketURL)
	}
	if err := database.ConnectMarket(marketURL, cfg.MarketTursoToken); err != nil {
		log.Fatalf("connect market db: %v", err)
	}
	if err := database.RunMigrationsOn(database.MarketDB, "database/migrations_market"); err != nil {
		log.Fatalf("market migrations: %v", err)
	}
	defer database.Close()

	ac := services.GetAlpacaClient()
	chunkDur := time.Duration(*chunkDays) * 24 * time.Hour

	totalBars := 0
	for _, symbol := range symbols {
		symBars := 0
		for chunkStart := start; chunkStart.Before(end); chunkStart = chunkStart.Add(chunkDur) {
			chunkEnd := chunkStart.Add(chunkDur)
			if chunkEnd.After(end) {
				chunkEnd = end
			}
			bars, err := ac.GetHistoricalCandles(symbol, "1Hour", chunkStart, chunkEnd)
			if err != nil {
				log.Printf("  %s [%s..%s]: %v", symbol, chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), err)
				continue
			}
			n, err := jobs.UpsertBars(symbol, bars)
			if err != nil {
				log.Printf("  %s upsert failed: %v", symbol, err)
				continue
			}
			symBars += n
		}
		log.Printf("%-6s: %d bars", symbol, symBars)
		totalBars += symBars
	}

	log.Printf("done: %d bars across %d symbols", totalBars, len(symbols))
}
