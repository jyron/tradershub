package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	// Single driver entry point: libsql owns both remote and local.
	// libsql-client-go handles libsql://, https://, http://, wss://, ws://
	// natively, and for file:// URLs it delegates to whichever sqlite/sqlite3
	// driver is registered. We import modernc.org/sqlite (pure Go, no CGO)
	// for that delegation — it's never opened directly.
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

// MarketDB is the second database — historical bars and frozen
// scenario_bars for the Benchmark API. Kept physically separate from the
// app DB so app-side schema changes can't accidentally touch market data,
// and so re-pulling bars from Alpaca isn't a recovery dependency for
// API keys, runs, results, and leaderboard rows.
var MarketDB *sql.DB

// Connect opens the SQLite/Turso database. Two URL forms are supported:
//   - "libsql://<host>" — remote Turso; authToken is appended as a query param.
//   - "file:./local.db" — local SQLite file; the libsql driver delegates to
//     modernc.org/sqlite internally. authToken is ignored.
//
// Use the local form for development so dev iteration doesn't burn Turso quota
// or risk dirtying shared state.
func Connect(databaseURL, authToken string) error {
	if databaseURL == "" {
		return fmt.Errorf("TURSO_DATABASE_URL is required (set to libsql://... for Turso or file:./bottrade.db for local)")
	}

	connStr := databaseURL
	if !isLocalURL(databaseURL) && authToken != "" {
		connStr = fmt.Sprintf("%s?authToken=%s", databaseURL, authToken)
	}

	db, err := sql.Open("libsql", connStr)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}

	if isLocalURL(databaseURL) {
		if err := applyLocalPragmas(db); err != nil {
			return fmt.Errorf("unable to apply SQLite pragmas: %w", err)
		}
	}

	DB = db
	log.Printf("Database connection established (%s)", redactURL(databaseURL))
	return nil
}

// applyLocalPragmas tunes the embedded SQLite for concurrent use by the Go
// server alongside Python bot scripts that open the same file.
//
//   - journal_mode=WAL lets readers and writers proceed without blocking each
//     other (default `delete` mode takes an exclusive lock per transaction).
//     The setting is persisted in the DB file, but re-applying is cheap.
//   - busy_timeout=5000 makes contended writes wait up to 5s for the lock
//     instead of immediately failing with SQLITE_BUSY (default 0).
//   - synchronous=NORMAL is the documented companion to WAL — full fsync per
//     commit isn't needed when WAL already journals durably.
func applyLocalPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

func isLocalURL(u string) bool {
	return strings.HasPrefix(u, "file:") || strings.HasPrefix(u, ":memory:")
}

// redactURL hides any sensitive parts of the connection string before logging.
func redactURL(u string) string {
	if i := strings.Index(u, "@"); i >= 0 {
		return u[:i] + "@…"
	}
	return u
}

// ConnectMarket opens the market-data Turso/SQLite database into
// the package-level MarketDB pool. Same URL conventions as Connect.
// Safe to call only in API mode; site-only mode skips this entirely.
func ConnectMarket(databaseURL, authToken string) error {
	if databaseURL == "" {
		return fmt.Errorf("TURSO_MARKET_DATABASE_URL is required for API mode")
	}

	connStr := databaseURL
	if !isLocalURL(databaseURL) && authToken != "" {
		connStr = fmt.Sprintf("%s?authToken=%s", databaseURL, authToken)
	}

	db, err := sql.Open("libsql", connStr)
	if err != nil {
		return fmt.Errorf("open market db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping market db: %w", err)
	}
	if isLocalURL(databaseURL) {
		if err := applyLocalPragmas(db); err != nil {
			return fmt.Errorf("apply pragmas to market db: %w", err)
		}
	}

	MarketDB = db
	log.Printf("Market DB connection established (%s)", redactURL(databaseURL))
	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
		log.Println("Database connection closed")
	}
	if MarketDB != nil {
		MarketDB.Close()
		log.Println("Market DB connection closed")
	}
}
