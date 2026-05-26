package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const protocolVersion = "2025-06-18"

type MCPServer struct {
	client *BotTradeClient
}

func NewMCPServer(client *BotTradeClient) *MCPServer {
	return &MCPServer{client: client}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

func (s *MCPServer) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		resp := s.handle(ctx, req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *MCPServer) handle(ctx context.Context, req request) response {
	id := rawID(req.ID)
	result, err := s.dispatch(ctx, req)
	if err != nil {
		return response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &rpcError{Code: -32000, Message: err.Error()},
		}
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func (s *MCPServer) dispatch(ctx context.Context, req request) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "bottrade-mcp",
				"version": "0.1.0",
			},
		}, nil
	case "tools/list":
		return map[string]any{"tools": tools()}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	case "resources/list":
		return map[string]any{"resources": resources()}, nil
	case "resources/read":
		return s.readResource(ctx, req.Params)
	case "prompts/list":
		return map[string]any{"prompts": prompts()}, nil
	case "prompts/get":
		return s.getPrompt(req.Params)
	case "ping":
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP method %q", req.Method)
	}
}

func (s *MCPServer) callTool(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	switch p.Name {
	case "connect_bottrade":
		return toolOK("Connect BotTrade to continue. Use the MCP client's Connect or Authorize button. If you need a browser sign-in page, open https://bot-trade.org/login. Do not open the MCP endpoint in a browser.", map[string]any{
			"status":      "authorization_required",
			"login_url":   "https://bot-trade.org/login",
			"account_url": "https://bot-trade.org/account",
			"instructions": []string{
				"Use the MCP client's Connect or Authorize button for BotTrade.",
				"Use https://bot-trade.org/login only as the human sign-in page.",
				"Do not send users to https://mcp.bot-trade.org/mcp in a browser; it is the agent protocol endpoint.",
			},
		}), nil
	case "list_scenarios":
		scenarios, err := s.client.ListScenarios(ctx)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeScenarios(scenarios), map[string]any{"scenarios": scenarios}), nil
	case "get_scenario":
		var args struct {
			IDOrSlug string `json:"id_or_slug"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		scenario, err := s.client.GetScenario(ctx, args.IDOrSlug)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(fmt.Sprintf("%s: %s (%d symbols)", scenario.Slug, scenario.Name, len(scenario.Universe)), scenario), nil
	case "start_run":
		var args struct {
			ScenarioSlug string `json:"scenario_slug"`
			BotName      string `json:"bot_name"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		run, err := s.client.StartRun(ctx, args.ScenarioSlug, args.BotName)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(fmt.Sprintf("Started run %s at %s", run.ID, run.SimTime.Format(timeFormat)), run), nil
	case "get_run":
		var args struct {
			RunID string `json:"run_id"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		snap, err := s.client.GetRun(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeRun(snap), snap), nil
	case "get_market":
		var args struct {
			RunID    string   `json:"run_id"`
			Symbols  []string `json:"symbols"`
			Lookback int      `json:"lookback"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		symbols := cleanSymbols(args.Symbols)
		if err := GuardRawMarketRequest(len(symbols), args.Lookback, len(symbols) == 0); err != nil {
			return toolErr(err), nil
		}
		market, err := s.client.GetMarket(ctx, args.RunID, symbols, args.Lookback)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeMarket(market), market), nil
	case "scan_market":
		var args struct {
			RunID string `json:"run_id"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		scan, err := s.client.ScanMarket(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(scan.HumanSummary, scan), nil
	case "inspect_symbols":
		var args struct {
			RunID    string   `json:"run_id"`
			Symbols  []string `json:"symbols"`
			Lookback int      `json:"lookback"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		inspection, err := s.client.InspectSymbols(ctx, args.RunID, args.Symbols, args.Lookback)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(inspection.HumanSummary, inspection), nil
	case "submit_turn":
		var args struct {
			RunID     string       `json:"run_id"`
			Trades    []TradeOrder `json:"trades"`
			StepCount int          `json:"step_count"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.SubmitTurn(ctx, args.RunID, args.Trades, args.StepCount)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeTurn(result), result), nil
	case "submit_decision":
		var args struct {
			RunID     string       `json:"run_id"`
			Action    string       `json:"action"`
			Rationale string       `json:"rationale"`
			Orders    []TradeOrder `json:"orders"`
			StepCount int          `json:"step_count"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.SubmitDecision(ctx, args.RunID, args.Action, args.Rationale, args.Orders, args.StepCount)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(result.HumanSummary, result), nil
	case "step_run":
		var args struct {
			RunID string `json:"run_id"`
			Count int    `json:"count"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		step, err := s.client.StepRun(ctx, args.RunID, args.Count)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeStep(step), step), nil
	case "get_results":
		var args struct {
			RunID string `json:"run_id"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		results, err := s.client.GetResults(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeResults(results), results), nil
	case "publish_run":
		var args struct {
			RunID   string `json:"run_id"`
			Confirm bool   `json:"confirm"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		if !args.Confirm {
			return toolErr(fmt.Errorf("publish_run requires confirm=true")), nil
		}
		results, err := s.client.PublishRun(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK("Published run to the BotTrade leaderboard.\n"+summarizeResults(results), results), nil
	default:
		return nil, fmt.Errorf("unknown tool %q", p.Name)
	}
}

const timeFormat = "2006-01-02T15:04:05Z"

func toolOK(summary string, structured any) toolResult {
	return toolResult{
		Content:           []content{{Type: "text", Text: summary}},
		StructuredContent: structured,
	}
}

func toolErr(err error) toolResult {
	return toolResult{
		Content: []content{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

func parseArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	return nil
}

func rawID(raw json.RawMessage) any {
	var id any
	if err := json.Unmarshal(raw, &id); err != nil {
		return string(raw)
	}
	return id
}

func cleanSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym != "" {
			out = append(out, sym)
		}
	}
	return out
}

func summarizeScenarios(scenarios []Scenario) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d BotTrade scenarios.", len(scenarios))
	for _, s := range scenarios {
		fmt.Fprintf(&b, "\n- %s: %s, %d symbols, %.0fx leverage, shorting=%t", s.Slug, s.Name, len(s.Universe), s.LeverageCap, s.ShortEnabled)
	}
	return b.String()
}

func summarizeRun(snap *RunSnapshot) string {
	equity := snap.Run.Cash
	if snap.LastEquity != nil {
		equity = snap.LastEquity.Equity
	}
	return fmt.Sprintf(
		"Run %s is %s at %s. Cash %.2f, equity %.2f, positions %d, queued orders %d.",
		snap.Run.ID,
		snap.Run.Status,
		snap.Run.SimTime.Format(timeFormat),
		snap.Run.Cash,
		equity,
		len(snap.Positions),
		len(snap.QueuedOrders),
	)
}

func summarizeMarket(market *MarketResponse) string {
	return fmt.Sprintf("Market snapshot at %s for %d symbols.", market.SimTime, len(market.Bars))
}

func summarizeStep(step *StepResult) string {
	return fmt.Sprintf(
		"Advanced %d bar(s) to %s. Fills %d, cash %.2f, equity %.2f, done=%t, liquidated=%t.",
		step.BarsAdvanced,
		step.NewSimTime.Format(timeFormat),
		len(step.Fills),
		step.NewCash,
		step.NewEquity,
		step.Done,
		step.Liquidated,
	)
}

func summarizeTurn(result *SubmitTurnResult) string {
	return fmt.Sprintf("Queued %d order(s). %s", len(result.QueuedOrders), summarizeStep(&result.Step))
}

func summarizeResults(results *RunResults) string {
	return fmt.Sprintf(
		"Final equity %.2f, return %.2f%%, trades %d, liquidated=%t.",
		results.FinalEquity,
		results.ReturnPct,
		results.TradeCount,
		results.Liquidated,
	)
}
