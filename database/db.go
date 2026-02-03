package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

var DB *sql.DB

func Connect(databaseURL, authToken string) error {
	// Turso connection string format: libsql://your-db.turso.io?authToken=xxx
	connStr := fmt.Sprintf("%s?authToken=%s", databaseURL, authToken)

	db, err := sql.Open("libsql", connStr)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}

	DB = db
	log.Println("Database connection established")
	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
		log.Println("Database connection closed")
	}
}
