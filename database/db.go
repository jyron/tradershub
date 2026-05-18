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

	DB = db
	log.Printf("Database connection established (%s)", redactURL(databaseURL))
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

func Close() {
	if DB != nil {
		DB.Close()
		log.Println("Database connection closed")
	}
}
