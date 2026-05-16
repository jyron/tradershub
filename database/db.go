package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

// Connect opens the SQLite/Turso database. Two URL forms are supported:
//   - "libsql://<host>" — remote Turso; an authToken is required and appended.
//   - "file:./local.db" — local SQLite file via the libsql driver; no token.
//
// Use the local form for development so dev iteration doesn't burn Turso quota
// or risk dirtying shared state.
func Connect(databaseURL, authToken string) error {
	if databaseURL == "" {
		return fmt.Errorf("TURSO_DATABASE_URL is required (set to libsql://... for Turso or file:./bottrade.db for local)")
	}

	driver := "libsql"
	connStr := databaseURL
	if strings.HasPrefix(databaseURL, "file:") || strings.HasPrefix(databaseURL, ":memory:") {
		// Local SQLite via modernc.org/sqlite (pure Go, no CGO). Keeps
		// dev iteration entirely off of Turso.
		driver = "sqlite"
	} else {
		connStr = fmt.Sprintf("%s?authToken=%s", databaseURL, authToken)
	}

	db, err := sql.Open(driver, connStr)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}

	DB = db
	log.Printf("Database connection established (%s)", redactURL(databaseURL))
	return nil
}

// redactURL hides any sensitive parts of the connection string before logging.
func redactURL(u string) string {
	if i := strings.Index(u, "@"); i >= 0 {
		return u[:i] + "@…"
	}
	return u
}

func Close() {
	if DB != nil {
		DB.Close()
		log.Println("Database connection closed")
	}
}
