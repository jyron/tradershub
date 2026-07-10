package apiv1

import (
	"database/sql"
	"path/filepath"
	"testing"

	"bottrade/database"

	"github.com/google/uuid"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// TestBackfillEncryptsLegacyPlaintextKeys simulates the production migration:
// an api_keys row created before encryption (plaintext in api_key, NULL hash)
// must, after the backfill, (1) no longer store the plaintext, (2) still
// authenticate, and (3) be idempotent on a second run.
func TestBackfillEncryptsLegacyPlaintextKeys(t *testing.T) {
	db, err := sql.Open("libsql", "file:"+filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	old := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = old })

	if err := database.RunMigrationsOn(db, "../../database/migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := InitKeyCipher("33333333333333333333333333333333333333333333333333333333333333ef"); err != nil {
		t.Fatalf("cipher: %v", err)
	}

	accountID := uuid.NewString()
	keyID := uuid.NewString()
	plaintext := "bt_legacy_" + uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO accounts (id, name, email, billing_email, plan) VALUES (?1, 'legacy', 'l@x.test', 'l@x.test', 'free')`,
		accountID,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	// Legacy row: plaintext in api_key, api_key_hash left NULL.
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, account_id, name, api_key, description, creator_email, plan)
		 VALUES (?1, ?2, 'legacy', ?3, '', 'l@x.test', 'free')`,
		keyID, accountID, plaintext,
	); err != nil {
		t.Fatalf("insert legacy key: %v", err)
	}

	if err := BackfillAPIKeyEncryption(db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var storedKey, storedHash string
	if err := db.QueryRow(`SELECT api_key, api_key_hash FROM api_keys WHERE id = ?1`, keyID).
		Scan(&storedKey, &storedHash); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if storedKey == plaintext {
		t.Fatal("plaintext key still stored after backfill")
	}
	if got := decryptSecret(storedKey); got != plaintext {
		t.Fatalf("decrypt(stored) = %q, want plaintext", got)
	}
	if storedHash != hashToken(plaintext) {
		t.Fatalf("api_key_hash = %q, want %q", storedHash, hashToken(plaintext))
	}

	// The legacy key still authenticates through the hashed-lookup path.
	loaded, err := loadAPIKeyBySecret(plaintext)
	if err != nil {
		t.Fatalf("loadAPIKeyBySecret after backfill: %v", err)
	}
	if loaded.ID.String() != keyID {
		t.Fatalf("loaded key id = %q, want %q", loaded.ID.String(), keyID)
	}
	if loaded.Key != plaintext {
		t.Fatalf("decrypted key = %q, want plaintext", loaded.Key)
	}

	// Idempotent: a second backfill leaves the ciphertext unchanged.
	if err := BackfillAPIKeyEncryption(db); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	var storedKey2 string
	_ = db.QueryRow(`SELECT api_key FROM api_keys WHERE id = ?1`, keyID).Scan(&storedKey2)
	if storedKey2 != storedKey {
		t.Fatal("second backfill re-encrypted an already-migrated row")
	}
}
