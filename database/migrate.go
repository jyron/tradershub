package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunMigrations applies all .sql files under database/migrations to the app DB
// in filename order. Kept as a thin wrapper so callers that only know about the
// app DB don't need to specify a path.
func RunMigrations() error {
	return RunMigrationsOn(DB, "database/migrations")
}

// RunMigrationsOn applies every .sql file under dir to the given database in
// lexicographic filename order. Filenames are expected to be numerically
// prefixed (e.g. 001_initial.sql) so the order is deterministic and stable.
//
// Each file is split into individual statements; a single "duplicate column"
// error per statement is treated as a no-op (SQLite lacks ADD COLUMN IF NOT
// EXISTS). Any other error aborts the run.
func RunMigrationsOn(db *sql.DB, dir string) error {
	if db == nil {
		return fmt.Errorf("RunMigrationsOn: nil db")
	}
	if err := ensureSchemaMigrations(db); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)

	for _, file := range files {
		name := filepath.Base(file)
		applied, err := migrationApplied(db, dir, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", file, err)
		}
		if err := execStatements(tx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
		if err := recordMigration(tx, dir, name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", file, err)
		}

		log.Printf("Executed migration: %s", file)
	}

	log.Printf("All migrations executed successfully (%s)", dir)
	return nil
}

func ensureSchemaMigrations(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			dir        TEXT NOT NULL,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (dir, name)
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func migrationApplied(db *sql.DB, dir, name string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE dir = ?1 AND name = ?2`,
		dir, name,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check schema migration %s/%s: %w", dir, name, err)
	}
	return n > 0, nil
}

type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func recordMigration(db sqlExecer, dir, name string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO schema_migrations (dir, name) VALUES (?1, ?2)`,
		dir, name,
	)
	if err != nil {
		return fmt.Errorf("record schema migration %s/%s: %w", dir, name, err)
	}
	return nil
}

// execStatements runs each ;-separated statement individually so a single
// "duplicate column" error (SQLite has no ADD COLUMN IF NOT EXISTS) doesn't
// abort the whole migration. All other errors propagate.
func execStatements(db sqlExecer, sql string) error {
	for _, stmt := range splitStatements(sql) {
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
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
