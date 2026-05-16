package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TursoDatabaseURL string
	TursoAuthToken   string
	Port             string
	MarketAPIKey     string
	AlpacaAPIKey     string
	AlpacaSecretKey  string
	AlpacaPaperMode  bool
	AdminSecret      string
}

func Load() *Config {
	// .env.local wins over .env (per dotenv conventions). Use this for
	// local SQLite + dev secrets without touching the shared Turso config.
	godotenv.Load(".env.local", ".env")

	return &Config{
		TursoDatabaseURL: os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:   os.Getenv("TURSO_AUTH_TOKEN"),
		Port:             getEnv("PORT", "3000"),
		MarketAPIKey:     getEnv("MARKET_API_KEY", ""),
		AlpacaAPIKey:     getEnv("ALPACA_API_KEY", ""),
		AlpacaSecretKey:  getEnv("ALPACA_SECRET_KEY", ""),
		AlpacaPaperMode:  getEnv("ALPACA_PAPER", "true") == "true",
		AdminSecret:      getEnv("ADMIN_SECRET", ""),
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
