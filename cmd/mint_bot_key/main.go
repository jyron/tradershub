// mint_bot_key creates a named benchmark-bot account and API key for
// leaderboard seeding. POST /api/v1/keys is gated behind a browser session,
// so operational seeding mints keys directly against the app database.
//
// Usage:
//
//	go run ./cmd/mint_bot_key --name "Claude Fable 5" [--plan free]
//
// The api_key line is printed once and never recoverable afterwards.
package main

import (
	"bottrade/config"
	"bottrade/database"
	apiv1 "bottrade/handlers/apiv1"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	name := flag.String("name", "", "bot display name (required)")
	plan := flag.String("plan", "free", "plan for the bot account")
	flag.Parse()
	if *name == "" {
		fmt.Fprintln(os.Stderr, `usage: mint_bot_key --name "Bot Name" [--plan free]`)
		os.Exit(2)
	}

	cfg := config.Load()
	if err := database.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken); err != nil {
		log.Fatalf("connect app db: %v", err)
	}
	defer database.Close()
	if err := apiv1.InitKeyCipher(cfg.AppEncryptionKey); err != nil {
		log.Fatalf("init key cipher: %v", err)
	}

	key, accountID, err := apiv1.MintBotKey(*name, *plan)
	if err != nil {
		log.Fatalf("mint key: %v", err)
	}
	fmt.Printf("account_id=%s\nname=%s\nplan=%s\napi_key=%s\n", accountID, *name, *plan, key)
}
