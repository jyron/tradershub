// provision_scenario creates a benchmark scenario from a JSON config file,
// snapshots its bars from market.bars into market.scenario_bars, and marks
// it ready. Bars must already be in `bars` (run cmd/backfill_bars first).
//
// Usage:
//   go run ./cmd/provision_scenario --config scenarios/tech-2024-q2.json
//
// Config schema (all top-level keys required unless noted):
//   {
//     "slug":              "tech-2024-q2",
//     "name":              "Tech sector, Q2 2024",
//     "description":       "...",                  // optional
//     "bar_resolution":    "1Hour",                // optional, default 1Hour
//     "start_ts":          "2024-04-01T13:00:00Z",
//     "end_ts":            "2024-06-30T20:00:00Z",
//     "starting_cash":     100000,                  // optional, default 100k
//     "leverage_cap":      4,                       // 1 | 2 | 4 | 10
//     "short_enabled":     true,
//     "universe":          ["AAPL","MSFT","NVDA"],  // or crypto: ["BTC/USD","ETH/USD"]
//     "slippage_bps":      {"PLTR":20},             // optional overrides
//     "benchmark_symbol":  "SPY"                    // optional, default SPY; use a
//                                                   // crypto pair (e.g. BTC/USD) for
//                                                   // a crypto scenario
//   }
//
// Bars for the window must already be in market.bars (run cmd/backfill_bars;
// crypto pairs are pulled from Alpaca's crypto feed automatically).
package main

import (
	"bottrade/config"
	"bottrade/database"
	"bottrade/services"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

type scenarioConfig struct {
	Slug            string         `json:"slug"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	BarResolution   string         `json:"bar_resolution"`
	StartTs         string         `json:"start_ts"`
	EndTs           string         `json:"end_ts"`
	StartingCash    float64        `json:"starting_cash"`
	LeverageCap     float64        `json:"leverage_cap"`
	ShortEnabled    bool           `json:"short_enabled"`
	Universe        []string       `json:"universe"`
	SlippageBps     map[string]int `json:"slippage_bps"`
	BenchmarkSymbol string         `json:"benchmark_symbol"`
}

func main() {
	var (
		configPath = flag.String("config", "", "path to scenario JSON config (required)")
	)
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: provision_scenario --config path/to/scenario.json")
		os.Exit(2)
	}

	rawJSON, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	var cfg scenarioConfig
	if err := json.Unmarshal(rawJSON, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}

	startTs, err := time.Parse(time.RFC3339, cfg.StartTs)
	if err != nil {
		log.Fatalf("parse start_ts: %v", err)
	}
	endTs, err := time.Parse(time.RFC3339, cfg.EndTs)
	if err != nil {
		log.Fatalf("parse end_ts: %v", err)
	}

	envCfg := config.Load()
	if err := database.Connect(envCfg.TursoDatabaseURL, envCfg.TursoAuthToken); err != nil {
		log.Fatalf("connect app db: %v", err)
	}
	if err := database.RunMigrations(); err != nil {
		log.Fatalf("app migrations: %v", err)
	}

	marketURL := envCfg.MarketTursoURL
	if marketURL == "" {
		marketURL = "file:./bottrade-market.db"
		log.Printf("Market DB URL not set, using local file %s", marketURL)
	}
	if err := database.ConnectMarket(marketURL, envCfg.MarketTursoToken); err != nil {
		log.Fatalf("connect market db: %v", err)
	}
	if err := database.RunMigrationsOn(database.MarketDB, "database/migrations_market"); err != nil {
		log.Fatalf("market migrations: %v", err)
	}
	defer database.Close()

	prov := services.NewScenarioProvisioner(database.DB, database.MarketDB)

	scen, err := prov.CreateScenario(services.CreateScenarioInput{
		Slug:            cfg.Slug,
		Name:            cfg.Name,
		Description:     cfg.Description,
		BarResolution:   cfg.BarResolution,
		StartTs:         startTs,
		EndTs:           endTs,
		StartingCash:    cfg.StartingCash,
		LeverageCap:     cfg.LeverageCap,
		ShortEnabled:    cfg.ShortEnabled,
		Universe:        cfg.Universe,
		SlippageBps:     cfg.SlippageBps,
		BenchmarkSymbol: cfg.BenchmarkSymbol,
	})
	if err != nil {
		log.Fatalf("create scenario: %v", err)
	}
	log.Printf("created scenario %s (slug=%s, id=%s)", scen.Name, scen.Slug, scen.ID)

	log.Printf("copying historical bars for %d symbols over [%s, %s)…", len(scen.Universe), cfg.StartTs, cfg.EndTs)
	n, err := prov.SnapshotScenario(scen.ID)
	if err != nil {
		log.Fatalf("snapshot: %v", err)
	}
	log.Printf("stored %d bars in scenario_bars (scenario_id=%s, version=%d). Status: ready.",
		n, scen.ID, scen.CurrentVersion)
}
