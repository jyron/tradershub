package services

import "strings"

// BenchmarkUniverse is the full set of symbols the bar-ingester pulls from
// Alpaca and the catalog of symbols a scenario's universe_json may reference.
// Scenarios pick subsets — a scenario doesn't have to use every symbol.
//
// Composition is sector-balanced (~30 single names, ~14 ETFs, ~5 high-variance)
// so scenarios can probe sector rotation and hedging behavior, not just
// "did the bot ride megacap tech".
//
// Editing this list expands what future scenarios can reference. Symbols
// removed here stay in scenario_bars for any already-provisioned scenario;
// the ingester just stops pulling new bars for them.
var BenchmarkUniverse = []string{
	// Tech megacaps (10)
	"AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "META", "AMD", "AVGO", "CRM", "ORCL",
	// Consumer (6)
	"TSLA", "HD", "COST", "NKE", "MCD", "SBUX",
	// Health (4)
	"UNH", "JNJ", "LLY", "PFE",
	// Finance (5)
	"JPM", "V", "MA", "GS", "BAC",
	// Energy / industrial (3)
	"XOM", "CVX", "CAT",
	// Communication / media (3)
	"NFLX", "DIS", "T",
	// Broad-market ETFs (4)
	"SPY", "QQQ", "IWM", "DIA",
	// Sector ETFs (6)
	"XLK", "XLF", "XLE", "XLV", "XLY", "XLP",
	// Thematic / hedge ETFs (4)
	"GLD", "TLT", "VXX", "USO",
	// High-variance single names (5)
	"PLTR", "COIN", "SMCI", "SHOP", "RBLX",
}

// CryptoUniverse is the set of crypto pairs the bar-ingester pulls from
// Alpaca's crypto feed and that a crypto scenario's universe_json may
// reference. Pairs use Alpaca's BASE/QUOTE form (e.g. "BTC/USD").
//
// Deliberately limited to liquid majors. Because a scenario's timeline is the
// union of all its symbols' bar timestamps, a thinly-traded pair with gaps
// would inject timestamps at which it has no bar — leaving orders to silently
// defer. Liquid majors keep the 24/7 grid dense.
var CryptoUniverse = []string{
	"BTC/USD", "ETH/USD", "SOL/USD", "LTC/USD", "BCH/USD",
	"LINK/USD", "UNI/USD", "AAVE/USD", "AVAX/USD", "DOGE/USD",
}

// IsCryptoSymbol reports whether a symbol is a crypto pair. Alpaca crypto
// pairs are written "BASE/QUOTE" (BTC/USD); equity tickers never contain a
// slash, so it's an unambiguous discriminator for routing ingest, backfill,
// and slippage defaults.
func IsCryptoSymbol(symbol string) bool {
	return strings.Contains(symbol, "/")
}

// DefaultSlippageBps returns the per-symbol slippage tier baked into a
// scenario at freeze time. Tiers:
//   - tight (3 bps): broad ETFs and SPY-grade liquidity
//   - medium (8 bps): megacap single names and sector ETFs
//   - wide (20 bps): high-variance / lower-liquidity / themed names
//   - crypto majors (5 bps for BTC/ETH, 18 bps for other pairs): crypto
//     spreads run wider than large-cap equities even for the deepest pairs.
//
// These are conservative defaults; a scenario can override per-symbol via
// its slippage_json field.
func DefaultSlippageBps(symbol string) int {
	switch symbol {
	// Tightest — full-volume ETFs
	case "SPY", "QQQ", "IWM", "DIA":
		return 3
	// Deepest crypto — major pairs
	case "BTC/USD", "ETH/USD":
		return 5
	// Widest — high-variance / themed / lower-cap
	case "PLTR", "COIN", "SMCI", "SHOP", "RBLX", "VXX", "USO":
		return 20
	default:
		if IsCryptoSymbol(symbol) {
			return 18
		}
		return 8
	}
}
