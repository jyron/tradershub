package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	// App DB — bots, scenarios catalog, runs, results, leaderboard.
	TursoDatabaseURL string
	TursoAuthToken   string

	// Market DB — historical bars + frozen scenario_bars. Separate Turso DB.
	MarketTursoURL   string
	MarketTursoToken string

	// Alpaca creds, used by the hourly bar-ingest job.
	AlpacaAPIKey    string
	AlpacaSecretKey string

	Port string
}

func Load() *Config {
	godotenv.Load(".env.local", ".env")

	return &Config{
		TursoDatabaseURL: os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:   os.Getenv("TURSO_AUTH_TOKEN"),
		MarketTursoURL:   os.Getenv("TURSO_MARKET_DATABASE_URL"),
		MarketTursoToken: os.Getenv("TURSO_MARKET_AUTH_TOKEN"),
		AlpacaAPIKey:     os.Getenv("ALPACA_API_KEY"),
		AlpacaSecretKey:  os.Getenv("ALPACA_SECRET_KEY"),
		Port:             getEnv("PORT", "3000"),
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
