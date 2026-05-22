package database

import (
	"fmt"
	"log"
	"os"
	"strings"
)

func RunMigrations() error {
	migrations := []string{
		"database/migrations/001_initial.sql",
		"database/migrations/002_add_claimed.sql",
		"database/migrations/003_add_is_test.sql",
		"database/migrations/004_add_assets.sql",
		"database/migrations/005_extend_symbol_columns.sql",
		"database/migrations/006_add_ranking_history.sql",
		"database/migrations/007_seasons.sql",
		"database/migrations/008_season_isolated_accounts.sql",
		"database/migrations/009_add_model_provider.sql",
		"database/migrations/010_bot_credentials_and_tiers.sql",
		"database/migrations/011_bot_usage_daily.sql",
		"database/migrations/012_backfill_jobs.sql",
		"database/migrations/013_add_is_baseline.sql",
		"database/migrations/014_daily_recaps.sql",
	}

	for _, migrationFile := range migrations {
		sqlBytes, err := os.ReadFile(migrationFile)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", migrationFile, err)
		}

		if err := execStatements(string(sqlBytes)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", migrationFile, err)
		}

		log.Printf("Executed migration: %s", migrationFile)
	}

	log.Println("All migrations executed successfully")
	return nil
}

// execStatements runs each ;-separated statement individually so a single
// "duplicate column" error (SQLite has no ADD COLUMN IF NOT EXISTS) doesn't
// abort the whole migration. All other errors propagate.
func execStatements(sql string) error {
	for _, stmt := range splitStatements(sql) {
		if stmt == "" {
			continue
		}
		if _, err := DB.Exec(stmt); err != nil {
			if isDuplicateColumnErr(err) {
				continue
			}
			return fmt.Errorf("statement failed: %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// splitStatements is a minimal SQL splitter — it splits on ';' outside of
// line comments. Assumption: no semicolons inside string literals or
// trigger bodies. All current migrations are simple DDL, so this is fine;
// if you add a CREATE TRIGGER or seed INSERT with literal text, revisit.
func splitStatements(sql string) []string {
	var out []string
	var current strings.Builder
	inLineComment := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if inLineComment {
			current.WriteByte(c)
			if c == '\n' {
				inLineComment = false
			}
			continue
		}
		if c == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			inLineComment = true
			current.WriteByte(c)
			continue
		}
		if c == ';' {
			out = append(out, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		out = append(out, trimmed)
	}
	return out
}

func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "already exists")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
