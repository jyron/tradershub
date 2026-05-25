package main

import (
	"context"
	"fmt"
	"math"
	"sort"
)

const (
	defaultScanLookback    = 8
	defaultInspectLookback = 30
	maxInspectSymbols      = 8
	maxInspectLookback     = 120
	maxRawMarketRows       = 500
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
	Symbol          string  `json:"symbol"`
	Close           float64 `json:"close"`
	BarReturnPct    float64 `json:"bar_return_pct,omitempty"`
	WindowReturnPct float64 `json:"window_return_pct,omitempty"`
	Volume          int64   `json:"volume"`
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
