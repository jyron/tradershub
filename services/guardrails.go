package services

import (
	"bottrade/database"
	"fmt"
	"time"
)

// Daily caps for challenger and verified tier bots. Official bots are
// exempt — they're hand-curated and we trust them.
const (
	DefaultMaxTradesPerDay = 20
	DefaultMaxLLMCallsDay  = 10
	// AutoDisableThreshold sets how many consecutive errors trip the
	// auto-disable; runners stop spawning the bot once this reaches the cap.
	AutoDisableThreshold = 5
)

func todayUTC() string {
	return time.Now().UTC().Format("2006-01-02")
}

// RecordTrade increments the bot's daily trade counter. Idempotency: each
// call adds one. Caller must already have validated the cap (see IsCapped).
func RecordTrade(botID string) error {
	return upsertCounter(botID, "trade_count")
}

// RecordLLMCall increments the bot's daily LLM-call counter. The Python
// adapter calls a thin endpoint to bump this so guardrails apply uniformly
// across replay + live modes.
func RecordLLMCall(botID string) error {
	return upsertCounter(botID, "llm_calls")
}

func upsertCounter(botID, column string) error {
	// INSERT … ON CONFLICT keeps the row's other counter intact.
	q := fmt.Sprintf(
		`INSERT INTO bot_usage_daily (bot_id, usage_date, %s)
		 VALUES (?, ?, 1)
		 ON CONFLICT(bot_id, usage_date)
		 DO UPDATE SET %s = bot_usage_daily.%s + 1`,
		column, column, column,
	)
	_, err := database.DB.Exec(q, botID, todayUTC())
	return err
}

// IsCapped reports whether the bot has already hit its daily cap for the
// given counter ("trade_count" or "llm_calls"). Pass tier so we can exempt
// official bots.
func IsCapped(botID, counter, tier string) (bool, error) {
	if tier == "official" {
		return false, nil
	}
	var cap int
	switch counter {
	case "trade_count":
		cap = DefaultMaxTradesPerDay
	case "llm_calls":
		cap = DefaultMaxLLMCallsDay
	default:
		return false, fmt.Errorf("unknown counter %q", counter)
	}
	q := fmt.Sprintf(
		`SELECT COALESCE(%s, 0) FROM bot_usage_daily WHERE bot_id = ? AND usage_date = ?`,
		counter,
	)
	var n int
	err := database.DB.QueryRow(q, botID, todayUTC()).Scan(&n)
	if err != nil {
		// No row yet → 0.
		return false, nil
	}
	return n >= cap, nil
}

// RecordError increments consecutive_errors and stashes the message. At
// AutoDisableThreshold the bot is auto-disabled.
func RecordError(botID, msg string) error {
	_, err := database.DB.Exec(
		`UPDATE bots
		    SET consecutive_errors = COALESCE(consecutive_errors, 0) + 1,
		        last_error         = ?,
		        last_run_at        = CURRENT_TIMESTAMP,
		        disabled_reason    = CASE
		            WHEN COALESCE(consecutive_errors, 0) + 1 >= ? THEN ?
		            ELSE disabled_reason
		        END
		  WHERE id = ?`,
		msg, AutoDisableThreshold,
		fmt.Sprintf("auto: %d consecutive errors", AutoDisableThreshold),
		botID,
	)
	return err
}

// ClearErrors zeroes the consecutive_errors counter; called after a
// successful run.
func ClearErrors(botID string) error {
	_, err := database.DB.Exec(
		`UPDATE bots
		    SET consecutive_errors = 0,
		        last_error         = NULL,
		        last_run_at        = CURRENT_TIMESTAMP
		  WHERE id = ?`,
		botID,
	)
	return err
}
