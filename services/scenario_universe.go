package services

// BenchmarkUniverse is the full set of symbols the bar-ingester pulls from
// Alpaca and the catalog of symbols a scenario's universe_json may reference.
// Scenarios pick subsets — a scenario doesn't have to use every symbol.
//
// Composition is sector-balanced (~30 single names, ~14 ETFs, ~5 high-variance)
// so scenarios can probe sector rotation and hedging behavior, not just
// "did the bot ride megacap tech".
//
// Editing this list expands what future scenarios can reference. Symbols
// removed here stay in scenario_bars for any already-frozen scenario;
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

// DefaultSlippageBps returns the per-symbol slippage tier baked into a
// scenario at freeze time. Three tiers:
//   - tight (3 bps): broad ETFs and SPY-grade liquidity
//   - medium (8 bps): megacap single names and sector ETFs
//   - wide (20 bps): high-variance / lower-liquidity / themed names
//
// These are conservative defaults; a scenario can override per-symbol via
// its slippage_json field.
func DefaultSlippageBps(symbol string) int {
	switch symbol {
	// Tightest — full-volume ETFs
	case "SPY", "QQQ", "IWM", "DIA":
		return 3
	// Widest — high-variance / themed / lower-cap
	case "PLTR", "COIN", "SMCI", "SHOP", "RBLX", "VXX", "USO":
		return 20
	default:
		return 8
	}
}
