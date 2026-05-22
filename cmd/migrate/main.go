// migrate applies SQL migrations to a Turso/SQLite database and exits.
// Use this for one-shot schema setup against remote DBs (e.g. pushing
// 015-018 to the prod app DB without restarting the live service, or
// initializing a brand-new market DB).
//
// Usage:
//   go run ./cmd/migrate --db-url 'libsql://...' --auth-token '...' --dir database/migrations
//   go run ./cmd/migrate --db-url 'file:./bottrade-market.db' --dir database/migrations_market
package main

import (
	"bottrade/database"
	"flag"
	"log"
	"os"
)

func main() {
	var (
		dbURL     = flag.String("db-url", "", "libsql:// or file:// URL (required)")
		authToken = flag.String("auth-token", "", "Turso auth token (ignored for file://)")
		dir       = flag.String("dir", "", "migrations directory (required)")
	)
	flag.Parse()

	if *dbURL == "" || *dir == "" {
		log.Println("usage: migrate --db-url URL --dir PATH [--auth-token TOKEN]")
		os.Exit(2)
	}

	// Choose which package-level pool to use. For arbitrary URLs we don't
	// care; the migrate runner takes a *sql.DB.
	if err := database.Connect(*dbURL, *authToken); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer database.Close()

	if err := database.RunMigrationsOn(database.DB, *dir); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("done.")
}
