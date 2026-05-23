package services

import (
	"bottrade/database"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// testScenarioSetup builds two in-memory SQLite databases (app + market),
// runs migrations, inserts a test bot + test scenario, and returns the
// scenario_id and engine.
//
// The scenario timeline is generated synthetically — `bars` lists per-bar
// (symbol, open, high, low, close) values starting at 2024-06-03T13:00Z
// and advancing one hour per index.
type testBar struct {
	Symbol             string
	Open, High, Low, Close float64
	Volume             int64
}

type testSetup struct {
	t          *testing.T
	appDBPath  string
	marketDBPath string
	AppDB      *sql.DB
	MarketDB   *sql.DB
	BotID      string
	ScenarioID string
	Engine     *ScenarioEngine
	Universe   []string
}

func newTestSetup(t *testing.T, universe []string, leverageCap float64, shortEnabled bool, slippage map[string]int, bars [][]testBar, startingCash float64) *testSetup {
	t.Helper()
	tmp := t.TempDir()
	appPath := filepath.Join(tmp, "app.db")
	marketPath := filepath.Join(tmp, "market.db")

	appDB, err := sql.Open("libsql", "file:"+appPath)
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })

	marketDB, err := sql.Open("libsql", "file:"+marketPath)
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	t.Cleanup(func() { marketDB.Close() })

	// Repo root for migrations: tests run from package dir, walk up.
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	if err := database.RunMigrationsOn(appDB, filepath.Join(repoRoot, "database/migrations")); err != nil {
		t.Fatalf("app migrations: %v", err)
	}
	if err := database.RunMigrationsOn(marketDB, filepath.Join(repoRoot, "database/migrations_market")); err != nil {
		t.Fatalf("market migrations: %v", err)
	}

	// Insert a bot row directly (the auth layer is bypassed in engine tests).
	botID := uuid.NewString()
	if _, err := appDB.Exec(`
		INSERT INTO bots (id, name, api_key, description, creator_email, is_active)
		VALUES (?1, 'test-bot', ?2, '', '', 1)
	`, botID, "test-key-"+botID); err != nil {
		t.Fatalf("insert bot: %v", err)
	}

	// Insert scenario.
	scenarioID := uuid.NewString()
	startTs := time.Date(2024, 6, 3, 13, 0, 0, 0, time.UTC)
	endTs := startTs.Add(time.Duration(len(bars)) * time.Hour)
	universeJSON := mustMarshalJSON(universe)
	if slippage == nil {
		slippage = map[string]int{}
		for _, sym := range universe {
			slippage[sym] = 5 // default 5 bps for tests unless overridden
		}
	}
	slippageJSON := mustMarshalJSON(slippage)
	shortInt := 0
	if shortEnabled {
		shortInt = 1
	}
	if _, err := appDB.Exec(`
		INSERT INTO scenarios (
			id, slug, name, bar_resolution, start_ts, end_ts,
			starting_cash, leverage_cap, short_enabled,
			universe_json, slippage_json, benchmark_symbol,
			status, current_version
		) VALUES (
			?1, ?2, 'Test scenario', '1Hour', ?3, ?4,
			?5, ?6, ?7,
			?8, ?9, ?10,
			'ready', 1
		)
	`,
		scenarioID, "test-"+scenarioID[:8],
		startTs.Format(time.RFC3339), endTs.Format(time.RFC3339),
		startingCash, leverageCap, shortInt,
		universeJSON, slippageJSON, universe[0],
	); err != nil {
		t.Fatalf("insert scenario: %v", err)
	}

	// Insert bars into market DB. bars[i] is the list of bars for hour i;
	// each entry maps a symbol to OHLCV.
	for i, hourBars := range bars {
		ts := startTs.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		for _, b := range hourBars {
			slip := slippage[b.Symbol]
			if _, err := marketDB.Exec(`
				INSERT INTO scenario_bars
					(scenario_id, scenario_version, symbol, ts, open, high, low, close, volume, slippage_bps)
				VALUES (?1, 1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
			`, scenarioID, b.Symbol, ts, b.Open, b.High, b.Low, b.Close, b.Volume, slip); err != nil {
				t.Fatalf("insert bar %d %s: %v", i, b.Symbol, err)
			}
		}
	}

	engine := NewScenarioEngine(appDB, marketDB)
	return &testSetup{
		t: t, appDBPath: appPath, marketDBPath: marketPath,
		AppDB: appDB, MarketDB: marketDB,
		BotID: botID, ScenarioID: scenarioID,
		Engine: engine, Universe: universe,
	}
}

func mustMarshalJSON(v interface{}) string {
	out, err := jsonMarshal(v)
	if err != nil {
		panic(err)
	}
	return out
}

func jsonMarshal(v interface{}) (string, error) {
	// Tiny wrapper to avoid adding encoding/json to the top-level imports.
	switch x := v.(type) {
	case []string:
		s := "["
		for i, sym := range x {
			if i > 0 {
				s += ","
			}
			s += `"` + sym + `"`
		}
		return s + "]", nil
	case map[string]int:
		s := "{"
		first := true
		for k, vv := range x {
			if !first {
				s += ","
			}
			first = false
			s += `"` + k + `":` + intToStr(vv)
		}
		return s + "}", nil
	}
	return "", fmt.Errorf("unsupported type")
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}

// findRepoRoot walks up from the test file's directory until it finds go.mod.
func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// ----------------------------------------------------------------------------
// Test cases
// ----------------------------------------------------------------------------

// Happy path: 10-bar scenario, agent buys 10 AAPL @ ~100, holds, price rises
// to 110 by end, expected equity = cash - 10*fillBuy + 10*100 + final mark-up.
func TestEngine_HappyPath(t *testing.T) {
	bars := make([][]testBar, 10)
	for i := 0; i < 10; i++ {
		// AAPL rises linearly from 100 to 109 over 10 bars.
		px := 100.0 + float64(i)
		bars[i] = []testBar{
			{Symbol: "AAPL", Open: px, High: px + 0.5, Low: px - 0.5, Close: px + 0.5, Volume: 1000},
		}
	}
	ts := newTestSetup(t, []string{"AAPL"}, 1.0, false, map[string]int{"AAPL": 0}, bars, 100000)

	run, err := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.Cash != 100000 {
		t.Errorf("starting cash mismatch: %v", run.Cash)
	}

	// Buy 10 AAPL: fills on bar 1 open (=101)
	if _, err := ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "AAPL", Side: "buy", Quantity: 10}); err != nil {
		t.Fatalf("QueueTrade buy: %v", err)
	}

	// Advance 9 bars (to bar idx 9 = last bar).
	stepResult, err := ts.Engine.AdvanceStep(run.ID, 9)
	if err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	if len(stepResult.Fills) != 1 {
		t.Errorf("expected 1 fill, got %d", len(stepResult.Fills))
	}
	fill := stepResult.Fills[0]
	if fill.FillPrice != 101.0 {
		t.Errorf("expected fill at 101 (bar 1 open), got %v", fill.FillPrice)
	}

	// At final bar (idx 9), AAPL close = 109.5. Position value = 10 * 109.5 = 1095.
	// Cash = 100000 - 10*101 = 98990.
	// Equity = 98990 + 1095 = 100085.
	if math.Abs(stepResult.NewEquity-100085.0) > 0.01 {
		t.Errorf("expected equity ~100085, got %v", stepResult.NewEquity)
	}
}

// Short path: open 5-share short @ ~100. Price rises to 110. Position
// value should be negative (we owe more than we received).
func TestEngine_Short(t *testing.T) {
	bars := make([][]testBar, 5)
	prices := []float64{100, 102, 105, 108, 110}
	for i, px := range prices {
		bars[i] = []testBar{{Symbol: "AAPL", Open: px, High: px + 1, Low: px - 1, Close: px, Volume: 1000}}
	}
	ts := newTestSetup(t, []string{"AAPL"}, 2.0, true, map[string]int{"AAPL": 0}, bars, 100000)
	run, _ := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)

	// Short 5 AAPL: fills on bar 1 open = 102. Cash += 5*102 = 510. Position qty = -5.
	if _, err := ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "AAPL", Side: "short", Quantity: 5}); err != nil {
		t.Fatalf("QueueTrade short: %v", err)
	}
	step, err := ts.Engine.AdvanceStep(run.ID, 4)
	if err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	if step.NewCash != 100510.0 {
		t.Errorf("expected cash 100510, got %v", step.NewCash)
	}
	// Position value at bar 4 (close=110): -5 * 110 = -550. Equity = 100510 - 550 = 99960.
	// Short lost ~$50 ($510 received minus $550 mark-to-market).
	if math.Abs(step.NewEquity-99960.0) > 0.01 {
		t.Errorf("expected equity ~99960, got %v", step.NewEquity)
	}
}

// Slippage: 5 bps buy fills at open * 1.0005.
func TestEngine_Slippage(t *testing.T) {
	bars := [][]testBar{
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 100, Volume: 1000}},
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 100, Volume: 1000}},
	}
	ts := newTestSetup(t, []string{"X"}, 1.0, false, map[string]int{"X": 5}, bars, 100000)
	run, _ := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)
	if _, err := ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "X", Side: "buy", Quantity: 1}); err != nil {
		t.Fatalf("QueueTrade: %v", err)
	}
	step, err := ts.Engine.AdvanceStep(run.ID, 1)
	if err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	expected := 100.0 * 1.0005
	if math.Abs(step.Fills[0].FillPrice-expected) > 1e-6 {
		t.Errorf("expected fill %.6f, got %v", expected, step.Fills[0].FillPrice)
	}
}

// 4x leverage liquidation: starts with $10k, buys $40k notional, then
// price drops 20% — equity should fall below maintenance and liquidation triggers.
func TestEngine_LeverageLiquidation(t *testing.T) {
	bars := make([][]testBar, 5)
	// Bar 0: open/close = 100. Bar 1: open=100, close=100. Bar 2: -20% to 80.
	prices := []float64{100, 100, 80, 80, 80}
	for i, px := range prices {
		bars[i] = []testBar{{Symbol: "X", Open: px, High: px + 1, Low: px - 1, Close: px, Volume: 1000}}
	}
	ts := newTestSetup(t, []string{"X"}, 4.0, false, map[string]int{"X": 0}, bars, 10000)
	run, _ := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)

	// Buy 400 shares @ ~$100 → notional = $40k. Required margin = 40000/4 = $10k. Cash = $10k. Just fits.
	if _, err := ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "X", Side: "buy", Quantity: 400}); err != nil {
		t.Fatalf("QueueTrade: %v", err)
	}

	// Advance 4 bars. Fill happens at bar 1 (price 100). Then bar 2 drops to 80.
	// Equity at bar 2: cash = 10000-40000=-30000, positions = 400*80=32000, equity=2000.
	// Notional = 32000. Maintenance = 32000/4/2 = 4000. Equity 2000 < 4000 → liquidate.
	step, err := ts.Engine.AdvanceStep(run.ID, 4)
	if err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	if !step.Liquidated {
		t.Fatalf("expected liquidation, did not get one. final equity %v", step.NewEquity)
	}
	if step.LiquidationAtTs == nil || step.LiquidationAtTs.Hour() != 15 { // bar idx 2 = 15:00
		t.Errorf("liquidation timestamp wrong: %v", step.LiquidationAtTs)
	}

	// Run should be marked liquidated
	run2, _ := ts.Engine.LoadRun(run.ID)
	if run2.Status != "liquidated" {
		t.Errorf("expected status=liquidated, got %v", run2.Status)
	}
}

// FIFO close: open long @ 100, partial close — avg_cost preserved, realized P&L on closed shares.
func TestEngine_PartialClose(t *testing.T) {
	bars := [][]testBar{
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 100, Volume: 1000}},
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 100, Volume: 1000}},
		{{Symbol: "X", Open: 110, High: 110, Low: 110, Close: 110, Volume: 1000}},
	}
	ts := newTestSetup(t, []string{"X"}, 1.0, false, map[string]int{"X": 0}, bars, 100000)
	run, _ := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)

	// Buy 10 → fills bar 1 @ 100.
	ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "X", Side: "buy", Quantity: 10})
	ts.Engine.AdvanceStep(run.ID, 1)

	// Sell 4 → fills bar 2 @ 110.
	ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "X", Side: "sell", Quantity: 4})
	step, err := ts.Engine.AdvanceStep(run.ID, 1)
	if err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	if step.Fills[0].FillPrice != 110.0 {
		t.Errorf("sell fill price = %v", step.Fills[0].FillPrice)
	}
	// realized = (110 - 100) * 4 = 40
	if math.Abs(step.Fills[0].RealizedPnL-40.0) > 0.01 {
		t.Errorf("realized PnL = %v, want 40", step.Fills[0].RealizedPnL)
	}
	// Remaining position: 6 shares, avg_cost still 100.
	positions, _ := ts.Engine.loadPositions(run.ID)
	if len(positions) != 1 || positions[0].Quantity != 6 || positions[0].AvgCost != 100.0 {
		t.Errorf("remaining position wrong: %+v", positions)
	}
}

// ComputeResults populates the row + idempotent re-runs.
func TestEngine_ComputeResults(t *testing.T) {
	bars := [][]testBar{
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 100, Volume: 1000}},
		{{Symbol: "X", Open: 100, High: 100, Low: 100, Close: 105, Volume: 1000}},
		{{Symbol: "X", Open: 105, High: 105, Low: 105, Close: 110, Volume: 1000}},
	}
	ts := newTestSetup(t, []string{"X"}, 1.0, false, map[string]int{"X": 0}, bars, 100000)
	run, _ := ts.Engine.StartRun(ts.BotID, ts.ScenarioID)
	ts.Engine.QueueTrade(run.ID, QueueTradeRequest{Symbol: "X", Side: "buy", Quantity: 100})
	if _, err := ts.Engine.AdvanceStep(run.ID, 5); err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}

	r, err := ts.Engine.ComputeResults(run.ID)
	if err != nil {
		t.Fatalf("ComputeResults: %v", err)
	}
	if r.TradeCount != 1 {
		t.Errorf("trade count = %d, want 1", r.TradeCount)
	}
	// Re-run is idempotent
	r2, err := ts.Engine.ComputeResults(run.ID)
	if err != nil {
		t.Fatalf("ComputeResults again: %v", err)
	}
	if r.FinalEquity != r2.FinalEquity {
		t.Errorf("non-idempotent compute: %v vs %v", r.FinalEquity, r2.FinalEquity)
	}
}
