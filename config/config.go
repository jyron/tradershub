package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	TursoDatabaseURL  string
	TursoAuthToken    string
	Port              string
	MarketAPIKey      string
	AlpacaAPIKey      string
	AlpacaSecretKey   string
	AlpacaPaperMode   bool
	AdminSecret       string
	// MasterKey is the active 32-byte hex AES-256 key used to encrypt
	// submitter LLM API keys at rest. Required for the submission flow.
	MasterKey         string
	// MasterKeyVersions maps version → hex key for in-flight rotation:
	// rows encrypted under an older version stay readable until the
	// rotation script rewrites them.
	MasterKeyVersions map[int]string
	// AnthropicAPIKey is the server-owned key used by the daily-recap job
	// to generate the natural-language summary. Optional — when unset the
	// recap falls back to a deterministic template.
	AnthropicAPIKey   string
}

func Load() *Config {
	// .env.local wins over .env (per dotenv conventions). Use this for
	// local SQLite + dev secrets without touching the shared Turso config.
	godotenv.Load(".env.local", ".env")

	return &Config{
		TursoDatabaseURL:  os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:    os.Getenv("TURSO_AUTH_TOKEN"),
		Port:              getEnv("PORT", "3000"),
		MarketAPIKey:      getEnv("MARKET_API_KEY", ""),
		AlpacaAPIKey:      getEnv("ALPACA_API_KEY", ""),
		AlpacaSecretKey:   getEnv("ALPACA_SECRET_KEY", ""),
		AlpacaPaperMode:   getEnv("ALPACA_PAPER", "true") == "true",
		AdminSecret:       getEnv("ADMIN_SECRET", ""),
		MasterKey:         getEnv("BOTTRADE_MASTER_KEY", ""),
		MasterKeyVersions: loadMasterKeyVersions(),
		AnthropicAPIKey:   getEnv("ANTHROPIC_API_KEY", ""),
	}
}

// loadMasterKeyVersions reads any BOTTRADE_MASTER_KEY_V<n> env vars
// (e.g. BOTTRADE_MASTER_KEY_V1) so old ciphertexts stay decryptable
// during rotation.
func loadMasterKeyVersions() map[int]string {
	out := map[int]string{}
	const prefix = "BOTTRADE_MASTER_KEY_V"
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k, v := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(k, prefix) || v == "" {
			continue
		}
		ver, err := strconv.Atoi(k[len(prefix):])
		if err != nil {
			continue
		}
		out[ver] = v
	}
	return out
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
