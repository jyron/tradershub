package apiv1

// MintBotKey creates a standalone benchmark-bot account with its own API key.
// Operational entry point for leaderboard seeding (cmd/mint_bot_key) now that
// POST /api/v1/keys requires a browser session. The plaintext key is returned
// exactly once; storage is encrypted like every user-issued key.
func MintBotKey(name, plan string) (apiKey, accountID string, err error) {
	resp, err := createAPIKey(name, "", plan)
	if err != nil {
		return "", "", err
	}
	return resp.APIKey, resp.AccountID, nil
}
