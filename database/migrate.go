package database

import (
	"fmt"
	"log"
	"os"
)

func RunMigrations() error {
	migrations := []string{
		"database/migrations/001_initial.sql",
		"database/migrations/002_add_claimed.sql",
		"database/migrations/003_add_is_test.sql",
		"database/migrations/004_add_assets.sql",
		"database/migrations/005_extend_symbol_columns.sql",
		"database/migrations/006_add_ranking_history.sql",
	}

	for _, migrationFile := range migrations {
		sqlBytes, err := os.ReadFile(migrationFile)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", migrationFile, err)
		}

		_, err = DB.Exec(string(sqlBytes))
		if err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", migrationFile, err)
		}

		log.Printf("Executed migration: %s", migrationFile)
	}

	log.Println("All migrations executed successfully")
	return nil
}
