package services

import (
	"bottrade/database"
	"fmt"
)

// VerifyAllCredentialsDecrypt walks bot_credentials and confirms every
// distinct key_version present has a corresponding active key in the vault.
// Called at server startup so an operator who removed an old
// BOTTRADE_MASTER_KEY_V<n> env without first re-encrypting the affected rows
// finds out at boot — not the first time the dynamic_bot_runner tries to
// decrypt a key.
func VerifyAllCredentialsDecrypt() error {
	if defaultVault == nil {
		return nil // vault disabled; submission flow is already off
	}
	rows, err := database.DB.Query(`SELECT DISTINCT key_version FROM bot_credentials`)
	if err != nil {
		return fmt.Errorf("keyvault preflight: %w", err)
	}
	defer rows.Close()
	var missing []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			continue
		}
		defaultVault.mu.RLock()
		_, ok := defaultVault.keys[v]
		defaultVault.mu.RUnlock()
		if !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"keyvault preflight: bot_credentials reference key_version(s) %v that aren't registered. "+
				"Set BOTTRADE_MASTER_KEY_V<n> for each missing version OR rotate those rows before removing the key.",
			missing,
		)
	}
	return nil
}
