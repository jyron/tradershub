package handlers

import (
	"bottrade/services"
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

// systemPromptPath returns the canonical location of bots/system_prompt.txt
// relative to whatever working directory the server was launched from. The
// file is the single source of truth for what every bot receives — the API
// reads the same bytes so the published methodology can't drift.
func systemPromptPath() string {
	return filepath.Join("bots", "system_prompt.txt")
}

// MethodologyVersion bumps when the rules, universe, or scoring change in a
// way that breaks comparability of historical results. Published in the JSON
// so researchers can cite a specific revision.
const MethodologyVersion = "2026.05.1"

// universe must stay in sync with bots/common.py:UNIVERSE. Kept here so the
// methodology page doesn't need to shell out to python to render it.
var universe = []string{
	"AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "META", "TSLA",
	"AMD", "NFLX", "SPY", "QQQ", "JPM", "XOM", "DIS", "PLTR",
}

// GetMethodology — GET /api/methodology
// Returns the full machine-readable description of the benchmark so it's
// trivially verifiable from outside.
func GetMethodology(c *fiber.Ctx) error {
	promptBytes, err := os.ReadFile(systemPromptPath())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "system prompt file unavailable",
			"details": err.Error(),
		})
	}
	return c.JSON(fiber.Map{
		"version":        MethodologyVersion,
		"system_prompt":  string(promptBytes),
		"starting_cash":  100000.0,
		"universe":       universe,
		"trade_cadence":  "Each bot receives one market snapshot per trading day and may emit up to 3 actions.",
		"position_sizing": fiber.Map{
			"max_per_new_buy_pct": 25.0,
			"target_positions":    "4–8",
			"long_only":           true,
			"shorting_allowed":    false,
			"margin_allowed":      false,
		},
		"data_sources": fiber.Map{
			"historical_bars": "Alpaca Markets — daily OHLC, IEX feed by default (SIP optional with paid plan).",
			"live_quotes":     "Finnhub.io — real-time bid/ask/last.",
		},
		"eligibility": fiber.Map{
			"min_trades_for_ranking": MinTradesForRanking,
			"note":                   "Bots with fewer executed trades than this are hidden from risk-adjusted boards until they meet the floor.",
		},
		"scoring": fiber.Map{
			"total_value":  "Cash plus mark-to-market positions, captured hourly into portfolio_snapshots.",
			"sharpe":       "Annualized daily returns divided by their standard deviation. Higher is better.",
			"sortino":      "Sharpe but only downside (negative) returns count toward the denominator. Higher is better.",
			"max_drawdown": "Largest peak-to-trough decline on the bot's snapshot series, as a percentage.",
		},
		"tiers": fiber.Map{
			"challenger": "Freshly submitted bot. Excluded from the default leaderboard until backfill completes and the auto-promotion fires.",
			"verified":   "Bot whose 30-day backfill landed cleanly. Default leaderboard tier. Trades daily during market hours.",
			"official":   "Hand-curated frontier benchmarks (the four flagship provider bots + synthetic baselines). Exempt from cleanup scripts and per-day trade caps.",
			"baseline":   "Marked separately with is_baseline=true. Deterministic reference strategies (SPY buy-and-hold, equal-weight, random walker).",
		},
		"guardrails": fiber.Map{
			"max_trades_per_day":     services.DefaultMaxTradesPerDay,
			"max_llm_calls_per_day":  services.DefaultMaxLLMCallsDay,
			"consecutive_error_cap":  services.AutoDisableThreshold,
			"note":                   "Per-bot daily caps protect submitters' LLM API keys from runaway behavior. Official-tier bots are exempt.",
		},
		"replay_methodology": fiber.Map{
			"days":  30,
			"feed":  "Alpaca daily bars (IEX). End-of-day close used as the trading price for replayed days; intraday timestamps within the trading window distinguish multiple actions on a single day.",
			"note":  "Replay wipes the bot's prior trades and snapshots before running — re-runs are deterministic given the same LLM output.",
		},
	})
}

// GetMethodologyPrompt — GET /api/methodology/prompt
// Plain-text endpoint so anyone reproducing a result can curl the *exact*
// bytes the bots see.
func GetMethodologyPrompt(c *fiber.Ctx) error {
	promptBytes, err := os.ReadFile(systemPromptPath())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("system prompt unavailable")
	}
	c.Set("Content-Type", "text/plain; charset=utf-8")
	c.Set("Cache-Control", "public, max-age=300")
	return c.Send(promptBytes)
}
