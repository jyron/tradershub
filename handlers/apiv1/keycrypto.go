package apiv1

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
)

// keyGCM encrypts BotTrade API keys at rest. The account's plaintext key is
// never stored: the api_keys table holds AES-256-GCM ciphertext (useless
// without the APP_ENCRYPTION_KEY held in the environment, not the DB) plus a
// SHA-256 hash (hashToken) for O(1) lookup. A leaked DB dump/backup/replica or
// a SQL-injection read therefore exposes no usable credentials.
var (
	keyCipherMu sync.RWMutex
	keyGCM      cipher.AEAD
)

// devEncryptionKey is a fixed, INSECURE key used only for local dev and tests
// when APP_ENCRYPTION_KEY is unset. main.go refuses to boot in production
// without a real key, so this never protects real data.
const devEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

// InitKeyCipher loads the at-rest cipher from a 64-char hex (32-byte) key.
// An empty key falls back to the insecure dev key (local/test only). Safe to
// call more than once.
func InitKeyCipher(hexKey string) error {
	if hexKey == "" {
		hexKey = devEncryptionKey
	}
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return fmt.Errorf("APP_ENCRYPTION_KEY must be hex-encoded: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("APP_ENCRYPTION_KEY must be 32 bytes (64 hex chars), got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	keyCipherMu.Lock()
	keyGCM = gcm
	keyCipherMu.Unlock()
	return nil
}

func keyCipher() cipher.AEAD {
	keyCipherMu.RLock()
	defer keyCipherMu.RUnlock()
	return keyGCM
}

// encryptSecret returns base64(nonce||ciphertext||tag) for a plaintext secret.
func encryptSecret(plaintext string) (string, error) {
	gcm := keyCipher()
	if gcm == nil {
		return "", fmt.Errorf("key cipher not initialized")
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// decryptSecret reverses encryptSecret. Returns "" if the blob can't be
// decrypted (e.g. APP_ENCRYPTION_KEY was rotated). Callers treat "" as "key
// not displayable" — authentication never depends on this, only on the hash.
func decryptSecret(blob string) string {
	gcm := keyCipher()
	if gcm == nil || blob == "" {
		return ""
	}
	sealed, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return ""
	}
	nonce := sealed[:gcm.NonceSize()]
	plaintext, err := gcm.Open(nil, nonce, sealed[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plaintext)
}

// BackfillAPIKeyEncryption migrates any api_keys row still holding a plaintext
// key (api_key_hash IS NULL) to hashed-lookup + encrypted-at-rest storage.
// Idempotent: already-migrated rows are skipped, so it is safe to run on every
// boot. The lookup hash is written from the plaintext in the same UPDATE that
// overwrites the column, so a row is always either fully-legacy or fully-migrated
// and authentication is never left in a broken state.
func BackfillAPIKeyEncryption(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, api_key FROM api_keys WHERE api_key_hash IS NULL OR api_key_hash = ''`)
	if err != nil {
		return err
	}
	type pending struct{ id, plaintext string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.plaintext); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range todo {
		enc, err := encryptSecret(p.plaintext)
		if err != nil {
			return err
		}
		if _, err := db.Exec(
			`UPDATE api_keys SET api_key_hash = ?1, api_key = ?2 WHERE id = ?3`,
			hashToken(p.plaintext), enc, p.id,
		); err != nil {
			return err
		}
	}
	if len(todo) > 0 {
		log.Printf("Encrypted %d API key(s) at rest", len(todo))
	}
	return nil
}
