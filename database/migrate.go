package database

import (
	"context"
	"fmt"
	"log"
	"os"
)

func RunMigrations() error {
	migrationFile := "database/migrations/001_initial.sql"

	sqlBytes, err := os.ReadFile(migrationFile)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	_, err = DB.Exec(context.Background(), string(sqlBytes))
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	log.Println("Migrations executed successfully")
	return nil
}
