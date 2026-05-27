package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	defaultScanLookback     = 8
	defaultInspectLookback  = 30
	maxInspectSymbols       = 8
	maxInspectLookback      = 120
	maxRawMarketRows        = 500
	defaultNextSessionBars  = 32
	defaultHoldUntilEndBars = 256
	maxAutonomousHoldBars   = 500
)

type DataBudget struct {
	SymbolsReturned int    `json:"symbols_returned"`
	BarsPerSymbol   int    `json:"bars_per_symbol"`
	EstimatedRows   int    `json:"estimated_rows"`
	Warning         string `json:"warning,omitempty"`
}

type ScanMarketResult struct {
	Phase               string       `json:"phase"`
	NextAction          string       `json:"next_action"`
	RunID               string       `json:"run_id"`
	SimTime             string       `json:"sim_time"`
	Symbols             []ScanSymbol `json:"symbols"`
	TopMovers           []string     `json:"top_movers"`
	SuggestedInspection []string     `json:"suggested_inspection"`
	DataBudget          DataBudget   `json:"data_budget"`
	HumanSummary        string       `json:"human_summary"`
}

type ScanSymbol struct {
	Symbol              string  `json:"symbol"`
	Close               float64 `json:"close"`
	BarReturnPct        float64 `json:"bar_return_pct"`
	WindowReturnPct     float64 `json:"window_return_pct"`
	Volume              int64   `json:"volume"`
	PositionQty         int     `json:"position_qty"`
	PositionAvgCost     float64 `json:"position_avg_cost"`
	PositionMarketValue float64 `json:"position_market_value"`
	UnrealizedPnL       float64 `json:"unrealized_pnl"`
	ExposurePct         float64 `json:"exposure_pct"`
}

type InspectSymbolsResult struct {
	Phase        string                 `json:"phase"`
	NextAction   string                 `json:"next_action"`
	RunID        string                 `json:"run_id"`
	SimTime      string                 `json:"sim_time"`
	Bars         map[string][]MarketBar `json:"bars"`
	DataBudget   DataBudget             `json:"data_budget"`
	HumanSummary string                 `json:"human_summary"`
}

type SubmitDecisionResult struct {
	Phase           string          `json:"phase"`
	NextAction      string          `json:"next_action"`
	Action          string          `json:"action"`
	Rationale       string          `json:"rationale,omitempty"`
	QueuedOrders    []QueuedOrder   `json:"queued_orders"`
	Step            StepResult      `json:"step"`
	Snapshot        *RunSnapshot    `json:"snapshot,omitempty"`
	Portfolio       map[string]any  `json:"portfolio,omitempty"`
	CompletionFlags map[string]bool `json:"completion_flags"`
	HumanSummary    string          `json:"human_summary"`
}

type AdvanceRunResult struct {
	Phase          string       `json:"phase"`
	NextAction     string       `json:"next_action"`
	RunID          string       `json:"run_id"`
	Mode           string       `json:"mode"`
	InitialSimTime string       `json:"initial_sim_time"`
	FinalSimTime   string       `json:"final_sim_time"`
	StepsSubmitted int          `json:"steps_submitted"`
	BarsAdvanced   int          `json:"bars_advanced"`
	FinalEquity    float64      `json:"final_equity"`
	FinalCash      float64      `json:"final_cash"`
	PositionsValue float64      `json:"positions_value"`
	Done           bool         `json:"done"`
	Liquidated     bool         `json:"liquidated"`
	ReachedLimit   bool         `json:"reached_limit"`
	Snapshot       *RunSnapshot `json:"snapshot,omitempty"`
	HumanSummary   string       `json:"human_summary"`
}

type LiquidateAndFinishResult struct {
	Phase        string            `json:"phase"`
	NextAction   string            `json:"next_action"`
	RunID        string            `json:"run_id"`
	ExitOrders   []TradeOrder      `json:"exit_orders"`
	ExitQueued   []QueuedOrder     `json:"exit_queued"`
	ExitStep     *StepResult       `json:"exit_step,omitempty"`
	Advance      *AdvanceRunResult `json:"advance,omitempty"`
	Snapshot     *RunSnapshot      `json:"snapshot,omitempty"`
	HumanSummary string            `json:"human_summary"`
}

type SandboxSmokeTestResult struct {
	Phase               string                `json:"phase"`
	NextAction          string                `json:"next_action"`
	RunID               string                `json:"run_id"`
	ScenarioSlug        string                `json:"scenario_slug"`
	Run                 *Run                  `json:"run"`
	ScanSimTime         string                `json:"scan_sim_time"`
	TopMovers           []string              `json:"top_movers"`
	SuggestedInspection []string              `json:"suggested_inspection"`
	DataBudget          DataBudget            `json:"data_budget"`
	Decision            *SubmitDecisionResult `json:"decision"`
	Published           bool                  `json:"published"`
	HumanSummary        string                `json:"human_summary"`
}

func (c *BotTradeClient) ScanMarket(ctx context.Context, runID string) (*ScanMarketResult, error) {
	snap, err := c.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	scenario, err := c.GetScenario(ctx, snap.Run.ScenarioID)
	if err != nil {
		return nil, err
	}
	market, err := c.GetMarket(ctx, runID, nil, defaultScanLookback)
	if err != nil {
		return nil, err
	}

	positions := positionMap(snap.Positions)
	equity := snap.Run.Cash
	if snap.LastEquity != nil && snap.LastEquity.Equity != 0 {
		equity = snap.LastEquity.Equity
	}
	rows := make([]ScanSymbol, 0, len(market.Bars))
	for _, symbol := range scenario.Universe {
		bars := market.Bars[symbol]
		if len(bars) == 0 {
			continue
		}
		latest := bars[len(bars)-1]
		row := ScanSymbol{
			Symbol: symbol,
			Close:  latest.Close,
			Volume: latest.Volume,
		}
		if pos, ok := positions[symbol]; ok {
			row.PositionQty = pos.Quantity
			row.PositionAvgCost = pos.AvgCost
			row.PositionMarketValue = float64(pos.Quantity) * latest.Close
			row.UnrealizedPnL = unrealizedPnL(pos, latest.Close)
			if equity != 0 {
				row.ExposurePct = row.PositionMarketValue / equity * 100
			}
		}
		if len(bars) >= 2 {
			prev := bars[len(bars)-2]
			row.BarReturnPct = pctChange(prev.Close, latest.Close)
		}
		first := bars[0]
		row.WindowReturnPct = pctChange(first.Close, latest.Close)
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		return math.Abs(rows[i].WindowReturnPct) > math.Abs(rows[j].WindowReturnPct)
	})
	topMovers := firstSymbols(rows, maxInspectSymbols)
	suggested := mergeSymbols(positionSymbols(snap.Positions), topMovers, maxInspectSymbols)

	return &ScanMarketResult{
		Phase:               "trading",
		NextAction:          "inspect_symbols_or_submit_decision",
		RunID:               runID,
		SimTime:             market.SimTime,
		Symbols:             rows,
		TopMovers:           topMovers,
		SuggestedInspection: suggested,
		DataBudget: DataBudget{
			SymbolsReturned: len(rows),
			BarsPerSymbol:   defaultScanLookback,
			EstimatedRows:   len(rows) * defaultScanLookback,
		},
		HumanSummary: fmt.Sprintf("Scanned %d symbols compactly at %s. Suggested inspection: %v.", len(rows), market.SimTime, suggested),
	}, nil
}

func (c *BotTradeClient) InspectSymbols(ctx context.Context, runID string, symbols []string, lookback int) (*InspectSymbolsResult, error) {
	symbols = cleanSymbols(symbols)
	if len(symbols) == 0 {
		return nil, fmt.Errorf("inspect_symbols requires 1-%d symbols; use scan_market first to choose them", maxInspectSymbols)
	}
	if len(symbols) > maxInspectSymbols {
		return nil, fmt.Errorf("inspect_symbols allows at most %d symbols; requested %d", maxInspectSymbols, len(symbols))
	}
	if lookback <= 0 {
		lookback = defaultInspectLookback
	}
	if lookback > maxInspectLookback {
		return nil, fmt.Errorf("inspect_symbols lookback is capped at %d bars; requested %d", maxInspectLookback, lookback)
	}
	market, err := c.GetMarket(ctx, runID, symbols, lookback)
	if err != nil {
		return nil, err
	}
	return &InspectSymbolsResult{
		Phase:      "trading",
		NextAction: "submit_decision",
		RunID:      runID,
		SimTime:    market.SimTime,
		Bars:       market.Bars,
		DataBudget: DataBudget{
			SymbolsReturned: len(market.Bars),
			BarsPerSymbol:   lookback,
			EstimatedRows:   len(market.Bars) * lookback,
		},
		HumanSummary: fmt.Sprintf("Inspected %d symbol(s) with %d-bar lookback at %s.", len(market.Bars), lookback, market.SimTime),
	}, nil
}

func (c *BotTradeClient) SubmitDecision(ctx context.Context, runID, action, rationale string, orders []TradeOrder, stepCount int) (*SubmitDecisionResult, error) {
	action = normalizeDecisionAction(action, len(orders))
	if action != "hold" && action != "trade" {
		return nil, fmt.Errorf("action must be hold or trade")
	}
	if action == "hold" && len(orders) > 0 {
		return nil, fmt.Errorf("action=hold cannot include orders; use action=trade or send no orders")
	}
	if action == "trade" && len(orders) == 0 {
		return nil, fmt.Errorf("action=trade requires at least one order; use action=hold to sit out")
	}
	turn, err := c.SubmitTurn(ctx, runID, orders, stepCount)
	if err != nil {
		return nil, err
	}

	phase := "trading"
	next := "scan_market"
	if turn.Step.Done || turn.Step.Liquidated {
		phase = "completed"
		next = "get_results"
	}

	portfolio := map[string]any{}
	if turn.Snapshot != nil {
		portfolio["cash"] = turn.Snapshot.Run.Cash
		portfolio["positions"] = turn.Snapshot.Positions
		if turn.Snapshot.LastEquity != nil {
			portfolio["equity"] = turn.Snapshot.LastEquity.Equity
			portfolio["positions_value"] = turn.Snapshot.LastEquity.PositionsValue
		}
	}

	return &SubmitDecisionResult{
		Phase:        phase,
		NextAction:   next,
		Action:       action,
		Rationale:    rationale,
		QueuedOrders: turn.QueuedOrders,
		Step:         turn.Step,
		Snapshot:     turn.Snapshot,
		Portfolio:    portfolio,
		CompletionFlags: map[string]bool{
			"done":       turn.Step.Done,
			"liquidated": turn.Step.Liquidated,
		},
		HumanSummary: fmt.Sprintf("%s decision submitted. %s Next: %s.", action, summarizeStep(&turn.Step), next),
	}, nil
}

func (c *BotTradeClient) AdvanceUntilNextSession(ctx context.Context, runID string, maxBars int) (*AdvanceRunResult, error) {
	return c.advanceWithoutTrades(ctx, runID, advanceOptions{
		mode:          "next_session",
		maxBars:       normalizeMaxBars(maxBars, defaultNextSessionBars),
		stopOnNewDate: true,
	})
}

func (c *BotTradeClient) HoldUntilEnd(ctx context.Context, runID string, maxBars int, requireFlat bool) (*AdvanceRunResult, error) {
	return c.advanceWithoutTrades(ctx, runID, advanceOptions{
		mode:        "hold_until_end",
		maxBars:     normalizeMaxBars(maxBars, defaultHoldUntilEndBars),
		requireFlat: requireFlat,
	})
}

func (c *BotTradeClient) LiquidateAndFinish(ctx context.Context, runID, rationale string, maxBars int) (*LiquidateAndFinishResult, error) {
	snap, err := c.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if snap.Run.Status != "active" {
		result := &LiquidateAndFinishResult{
			Phase:        "completed",
			NextAction:   "get_results",
			RunID:        runID,
			Snapshot:     snap,
			HumanSummary: "Run is already finished; no liquidation orders were queued. Next: get_results.",
		}
		return result, nil
	}

	exitOrders := flattenOrders(snap.Positions, rationale)
	if rationale == "" {
		rationale = "Flatten existing positions before finishing the run."
	}
	result := &LiquidateAndFinishResult{
		Phase:      "trading",
		NextAction: "hold_until_end",
		RunID:      runID,
		ExitOrders: exitOrders,
		Snapshot:   snap,
	}
	if len(exitOrders) > 0 {
		turn, err := c.SubmitTurn(ctx, runID, exitOrders, 1)
		if err != nil {
			return nil, err
		}
		result.ExitQueued = turn.QueuedOrders
		result.ExitStep = &turn.Step
		result.Snapshot = turn.Snapshot
		if turn.Step.Done || turn.Step.Liquidated {
			result.Phase = "completed"
			result.NextAction = "get_results"
			result.HumanSummary = fmt.Sprintf("Flattened with %d exit order(s). %s Next: get_results.", len(exitOrders), summarizeStep(&turn.Step))
			return result, nil
		}
	}

	advance, err := c.HoldUntilEnd(ctx, runID, maxBars, true)
	if err != nil {
		return nil, err
	}
	result.Advance = advance
	result.Snapshot = advance.Snapshot
	result.Phase = advance.Phase
	result.NextAction = advance.NextAction
	result.HumanSummary = fmt.Sprintf("Flattened with %d exit order(s), then held cash. %s", len(exitOrders), advance.HumanSummary)
	return result, nil
}

func (c *BotTradeClient) RunSandboxSmokeTest(ctx context.Context, scenarioSlug, botName string) (*SandboxSmokeTestResult, error) {
	if strings.TrimSpace(scenarioSlug) == "" {
		scenarioSlug = "sandbox-nov-2024"
	}
	if strings.TrimSpace(botName) == "" {
		botName = "MCP sandbox smoke test"
	}
	run, err := c.StartRun(ctx, scenarioSlug, botName)
	if err != nil {
		return nil, err
	}
	scan, err := c.ScanMarket(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	decision, err := c.SubmitDecision(ctx, run.ID, "hold", "MCP smoke test hold; no strategy decision.", nil, 1)
	if err != nil {
		return nil, err
	}
	result := &SandboxSmokeTestResult{
		Phase:               decision.Phase,
		NextAction:          decision.NextAction,
		RunID:               run.ID,
		ScenarioSlug:        scenarioSlug,
		Run:                 run,
		ScanSimTime:         scan.SimTime,
		TopMovers:           scan.TopMovers,
		SuggestedInspection: scan.SuggestedInspection,
		DataBudget:          scan.DataBudget,
		Decision:            decision,
		Published:           false,
		HumanSummary:        fmt.Sprintf("Sandbox smoke test created run %s, scanned %d symbols, submitted one hold step, and did not publish. Next: %s.", run.ID, scan.DataBudget.SymbolsReturned, decision.NextAction),
	}
	return result, nil
}

func GuardRawMarketRequest(symbolCount, lookback int, symbolsOmitted bool) error {
	if lookback <= 0 {
		lookback = 50
	}
	if symbolsOmitted && lookback > 1 {
		return fmt.Errorf("raw get_market with all symbols is capped at lookback=1; use scan_market, then inspect_symbols for selected symbols")
	}
	if symbolCount > maxInspectSymbols {
		return fmt.Errorf("raw get_market allows at most %d explicit symbols; use scan_market first", maxInspectSymbols)
	}
	estimated := symbolCount * lookback
	if estimated > maxRawMarketRows {
		return fmt.Errorf("raw get_market request is too large (%d estimated rows, max %d); use scan_market then inspect_symbols", estimated, maxRawMarketRows)
	}
	return nil
}

type advanceOptions struct {
	mode          string
	maxBars       int
	stopOnNewDate bool
	requireFlat   bool
}

func (c *BotTradeClient) advanceWithoutTrades(ctx context.Context, runID string, opts advanceOptions) (*AdvanceRunResult, error) {
	if opts.maxBars <= 0 {
		opts.maxBars = defaultHoldUntilEndBars
	}
	if opts.maxBars > maxAutonomousHoldBars {
		opts.maxBars = maxAutonomousHoldBars
	}
	snap, err := c.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if opts.requireFlat && hasOpenPositions(snap.Positions) {
		return nil, fmt.Errorf("%s requires a flat portfolio; use liquidate_and_finish or set require_flat=false", opts.mode)
	}
	if len(snap.QueuedOrders) > 0 {
		return nil, fmt.Errorf("%s will not advance while %d queued order(s) are pending; use submit_decision or step_run explicitly", opts.mode, len(snap.QueuedOrders))
	}

	initial := snap.Run.SimTime
	finalSim := initial
	finalEquity := snap.Run.Cash
	finalCash := snap.Run.Cash
	positionsValue := 0.0
	if snap.LastEquity != nil {
		finalEquity = snap.LastEquity.Equity
		positionsValue = snap.LastEquity.PositionsValue
	}
	done := snap.Run.Status != "active"
	liquidated := snap.Run.Status == "liquidated"
	stepsSubmitted := 0
	barsAdvanced := 0
	reachedLimit := false

	for !done && stepsSubmitted < opts.maxBars {
		step, err := c.StepRun(ctx, runID, 1)
		if err != nil {
			return nil, err
		}
		stepsSubmitted++
		barsAdvanced += step.BarsAdvanced
		finalSim = step.NewSimTime
		finalEquity = step.NewEquity
		finalCash = step.NewCash
		positionsValue = step.PositionsValue
		done = step.Done
		liquidated = step.Liquidated
		if opts.stopOnNewDate && !sameTradingDate(initial, finalSim) {
			break
		}
		if step.Done || step.Liquidated || step.BarsAdvanced == 0 {
			break
		}
	}
	if !done && stepsSubmitted >= opts.maxBars {
		reachedLimit = true
	}
	snap, _ = c.GetRun(ctx, runID)
	if snap != nil {
		finalSim = snap.Run.SimTime
		finalCash = snap.Run.Cash
		if snap.LastEquity != nil {
			finalEquity = snap.LastEquity.Equity
			positionsValue = snap.LastEquity.PositionsValue
		}
		done = snap.Run.Status != "active"
		liquidated = snap.Run.Status == "liquidated"
	}

	phase := "trading"
	next := "scan_market"
	if done || liquidated {
		phase = "completed"
		next = "get_results"
	}
	summary := fmt.Sprintf("%s submitted %d hold step(s), advanced %d bar(s), equity %.2f. Done=%t. Liquidated=%t. Next: %s.",
		opts.mode, stepsSubmitted, barsAdvanced, finalEquity, done, liquidated, next)
	if reachedLimit {
		summary = fmt.Sprintf("%s Reached max_bars=%d before completion.", summary, opts.maxBars)
	}
	return &AdvanceRunResult{
		Phase:          phase,
		NextAction:     next,
		RunID:          runID,
		Mode:           opts.mode,
		InitialSimTime: initial.Format(time.RFC3339),
		FinalSimTime:   finalSim.Format(time.RFC3339),
		StepsSubmitted: stepsSubmitted,
		BarsAdvanced:   barsAdvanced,
		FinalEquity:    finalEquity,
		FinalCash:      finalCash,
		PositionsValue: positionsValue,
		Done:           done,
		Liquidated:     liquidated,
		ReachedLimit:   reachedLimit,
		Snapshot:       snap,
		HumanSummary:   summary,
	}, nil
}

func normalizeMaxBars(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value > maxAutonomousHoldBars {
		return maxAutonomousHoldBars
	}
	return value
}

func sameTradingDate(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

func positionMap(positions []Position) map[string]Position {
	out := make(map[string]Position, len(positions))
	for _, position := range positions {
		if position.Quantity != 0 {
			out[position.Symbol] = position
		}
	}
	return out
}

func hasOpenPositions(positions []Position) bool {
	for _, position := range positions {
		if position.Quantity != 0 {
			return true
		}
	}
	return false
}

func unrealizedPnL(position Position, close float64) float64 {
	if position.Quantity > 0 {
		return (close - position.AvgCost) * float64(position.Quantity)
	}
	if position.Quantity < 0 {
		return (position.AvgCost - close) * float64(-position.Quantity)
	}
	return 0
}

func flattenOrders(positions []Position, rationale string) []TradeOrder {
	if rationale == "" {
		rationale = "Flatten existing position."
	}
	orders := make([]TradeOrder, 0, len(positions))
	for _, position := range positions {
		if position.Quantity > 0 {
			orders = append(orders, TradeOrder{
				Symbol:    position.Symbol,
				Side:      "sell",
				Quantity:  position.Quantity,
				Reasoning: rationale,
			})
		}
		if position.Quantity < 0 {
			orders = append(orders, TradeOrder{
				Symbol:    position.Symbol,
				Side:      "cover",
				Quantity:  -position.Quantity,
				Reasoning: rationale,
			})
		}
	}
	return orders
}

func pctChange(from, to float64) float64 {
	if from == 0 {
		return 0
	}
	return (to - from) / from * 100
}

func firstSymbols(rows []ScanSymbol, limit int) []string {
	out := make([]string, 0, limit)
	for _, row := range rows {
		if len(out) >= limit {
			break
		}
		out = append(out, row.Symbol)
	}
	return out
}

func positionSymbols(positions []Position) []string {
	out := make([]string, 0, len(positions))
	for _, p := range positions {
		if p.Quantity != 0 {
			out = append(out, p.Symbol)
		}
	}
	return out
}

func mergeSymbols(primary, secondary []string, limit int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, limit)
	add := func(symbol string) {
		if len(out) >= limit || symbol == "" || seen[symbol] {
			return
		}
		seen[symbol] = true
		out = append(out, symbol)
	}
	for _, symbol := range primary {
		add(symbol)
	}
	for _, symbol := range secondary {
		add(symbol)
	}
	return out
}

func normalizeDecisionAction(action string, orderCount int) string {
	switch action {
	case "hold", "trade":
		return action
	case "":
		if orderCount == 0 {
			return "hold"
		}
		return "trade"
	default:
		return action
	}
}
