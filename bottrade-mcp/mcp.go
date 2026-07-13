package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/posthog/posthog-go"
	"io"
	"strings"
	"time"
)

const protocolVersion = "2025-06-18"

type MCPServer struct {
	client    *BotTradeClient
	session   *MCPSession
	auth      *OAuthBridge
	analytics *Analytics
	transport string
	clientIP  string // real caller IP for PostHog GeoIP; empty on stdio
}

// distinctID is a stable, non-secret PostHog identity for the caller: a hash of
// the API key when present, else the MCP session id, else anonymous. We never
// send the raw API key.
func (s *MCPServer) distinctID() string {
	if s.client != nil {
		if k := strings.TrimSpace(s.client.apiKey); k != "" {
			sum := sha256.Sum256([]byte(k))
			return "key_" + hex.EncodeToString(sum[:])[:24]
		}
	}
	if s.session != nil && s.session.ID != "" {
		return "sess_" + s.session.ID
	}
	return "mcp_anon"
}

func NewMCPServer(client *BotTradeClient) *MCPServer {
	return &MCPServer{client: client}
}

func NewMCPServerWithAuth(client *BotTradeClient, session *MCPSession, auth *OAuthBridge) *MCPServer {
	return &MCPServer{client: client, session: session, auth: auth}
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
	Annotations map[string]any `json:"annotations,omitempty"`
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
		s.analytics.Capture(s.distinctID(), "mcp_session_initialized", s.baseProps())
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

	s.analytics.Capture(s.distinctID(), "mcp_tool_called", s.baseProps().
		Set("tool", p.Name))

	switch p.Name {
	case "auth_status":
		status := s.authStatus()
		return toolOK("Auth status: "+status["status"].(string)+".", status), nil
	case "connect_bottrade":
		var args struct {
			WaitSeconds int `json:"wait_seconds"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		if s.auth == nil || s.session == nil {
			return toolOK("Auth required.", authRequiredPayload("")), nil
		}
		status, err := s.auth.Start(ctx, s.session)
		if err != nil {
			return toolErr(err), nil
		}
		if status["status"] != "connected" && !s.session.LoginShown {
			s.auth.store.MarkLoginShown(s.session.ID)
			loginURL, _ := status["login_url"].(string)
			return toolOK("Open this URL: "+loginURL, status), nil
		}
		if args.WaitSeconds > 0 {
			if args.WaitSeconds > 120 {
				args.WaitSeconds = 120
			}
			deadline := time.Now().Add(time.Duration(args.WaitSeconds) * time.Second)
			for s.session.ValidToken() == "" && time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return toolErr(ctx.Err()), nil
				case <-time.After(time.Second):
				}
			}
			if s.session.ValidToken() != "" {
				return toolOK("Connected.", s.authStatus()), nil
			}
		}
		if status["status"] == "connected" {
			return toolOK("Connected.", s.authStatus()), nil
		}
		loginURL, _ := status["login_url"].(string)
		return toolOK("Open this URL: "+loginURL, status), nil
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
			ScenarioSlug string     `json:"scenario_slug"`
			BotName      string     `json:"bot_name"`
			AgentInfo    *AgentInfo `json:"agent_info"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		run, err := s.client.StartRunWithAgentInfo(
			ctx, args.ScenarioSlug, args.BotName, args.AgentInfo,
		)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(fmt.Sprintf("Started %s.", run.ID), run), nil
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
		if err := validateLoopStepCount("submit_turn", args.StepCount); err != nil {
			return toolErr(err), nil
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
		if err := validateLoopStepCount("submit_decision", args.StepCount); err != nil {
			return toolErr(err), nil
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
		if err := validateLoopStepCount("step_run", args.Count); err != nil {
			return toolErr(err), nil
		}
		step, err := s.client.StepRun(ctx, args.RunID, args.Count)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(summarizeStep(step), step), nil
	case "advance_until_next_session":
		var args struct {
			RunID   string `json:"run_id"`
			MaxBars int    `json:"max_bars"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.AdvanceUntilNextSession(ctx, args.RunID, args.MaxBars)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(result.HumanSummary, result), nil
	case "hold_until_end":
		var args struct {
			RunID       string `json:"run_id"`
			MaxBars     int    `json:"max_bars"`
			RequireFlat bool   `json:"require_flat"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.HoldUntilEnd(ctx, args.RunID, args.MaxBars, args.RequireFlat)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(result.HumanSummary, result), nil
	case "liquidate_and_finish":
		var args struct {
			RunID     string `json:"run_id"`
			Rationale string `json:"rationale"`
			MaxBars   int    `json:"max_bars"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.LiquidateAndFinish(ctx, args.RunID, args.Rationale, args.MaxBars)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(result.HumanSummary, result), nil
	case "run_sandbox_smoke_test":
		var args struct {
			ScenarioSlug string `json:"scenario_slug"`
			BotName      string `json:"bot_name"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		result, err := s.client.RunSandboxSmokeTest(ctx, args.ScenarioSlug, args.BotName)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(result.HumanSummary, result), nil
	case "get_results":
		var args struct {
			RunID string `json:"run_id"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		results, err := s.client.GetResultsDetail(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(results.HumanSummary, results), nil
	case "get_trades":
		var args struct {
			RunID string `json:"run_id"`
		}
		if err := parseArgs(p.Arguments, &args); err != nil {
			return nil, err
		}
		trades, err := s.client.GetTrades(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK(fmt.Sprintf("%d filled trade(s).", len(trades)), map[string]any{"run_id": args.RunID, "trades": trades}), nil
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
		result, err := s.client.PublishRun(ctx, args.RunID)
		if err != nil {
			return toolErr(err), nil
		}
		return toolOK("Published. "+summarizeResults(&result.Results)+" Public URL: "+result.PublicURL, result), nil
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
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		return toolResult{
			Content:           []content{{Type: "text", Text: authErr.Error()}},
			StructuredContent: authRequiredPayload(authErr.LoginURL),
			IsError:           true,
		}
	}
	return toolResult{
		Content: []content{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

func authRequiredPayload(loginURL string) map[string]any {
	payload := map[string]any{
		"error":        "auth_required",
		"status":       "auth_required",
		"next_action":  "connect_bottrade",
		"scope":        "bottrade:trade",
		"auth_methods": []string{"oauth", "authorization_bearer", "x_api_key", "BOTTRADE_API_KEY"},
		"message":      "Call connect_bottrade to start OAuth, reuse the returned Mcp-Session-Id after sign-in, or provide a BotTrade API key as Authorization: Bearer or X-API-Key.",
	}
	if loginURL != "" {
		payload["login_url"] = loginURL
	}
	return payload
}

func (s *MCPServer) authStatus() map[string]any {
	if s.client != nil && strings.TrimSpace(s.client.apiKey) != "" {
		return map[string]any{
			"status":      "connected",
			"auth_method": "api_key_or_bearer",
		}
	}
	if s.session != nil {
		if token := s.session.ValidToken(); token != "" {
			return map[string]any{
				"status":      "connected",
				"auth_method": "oauth",
				"expires_at":  s.session.ExpiresAt.Format(time.RFC3339),
			}
		}
		if s.session.LoginURL != "" {
			loginURL := s.session.LoginURL
			if s.auth != nil {
				loginURL = strings.TrimRight(s.auth.publicURL, "/") + "/connect/" + s.session.ID
			}
			return map[string]any{
				"status":      "pending",
				"auth_method": "oauth",
				"login_url":   loginURL,
				"next_action": "open_login_url",
			}
		}
	}
	status := authRequiredPayload("")
	status["auth_method"] = "none"
	return status
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

func validateLoopStepCount(toolName string, count int) error {
	if count == 0 {
		return nil
	}
	if count < 0 {
		return fmt.Errorf("%s count must be at least 1", toolName)
	}
	if count > 1 {
		return fmt.Errorf("%s only supports count=1 in MCP; repeat the loop one bar at a time so the bot can observe and trade before the run ends", toolName)
	}
	return nil
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
	fmt.Fprintf(&b, "%d scenarios.", len(scenarios))
	for _, s := range scenarios {
		fmt.Fprintf(&b, "\n- %s: %s", s.Slug, s.Name)
	}
	return b.String()
}

func summarizeRun(snap *RunSnapshot) string {
	equity := snap.Run.Cash
	if snap.LastEquity != nil {
		equity = snap.LastEquity.Equity
	}
	return fmt.Sprintf(
		"%s %s. Equity %.2f. Positions %d. Orders %d.",
		snap.Run.ID,
		snap.Run.Status,
		equity,
		len(snap.Positions),
		len(snap.QueuedOrders),
	)
}

func summarizeMarket(market *MarketResponse) string {
	return fmt.Sprintf("%d symbols.", len(market.Bars))
}

func summarizeStep(step *StepResult) string {
	return fmt.Sprintf(
		"Step +%d. Equity %.2f. Fills %d. Done=%t. Liquidated=%t.",
		step.BarsAdvanced,
		step.NewEquity,
		len(step.Fills),
		step.Done,
		step.Liquidated,
	)
}

func summarizeTurn(result *SubmitTurnResult) string {
	return fmt.Sprintf("%d orders. %s", len(result.QueuedOrders), summarizeStep(&result.Step))
}

func summarizeResults(results *RunResults) string {
	return fmt.Sprintf(
		"Equity %.2f. Return %.2f%%. Trades %d. Liquidated=%t.",
		results.FinalEquity,
		results.ReturnPct,
		results.TradeCount,
		results.Liquidated,
	)
}

// baseProps returns the property bag every MCP event carries: transport plus
// the caller's real IP ($ip) so PostHog GeoIP resolves agent operators too.
//
// Sessions without an API key get $process_person_profile:false — their
// distinct_id is a per-connection session id (clients reconnect every 30-min
// TTL), so letting each one create a person profile floods analytics with
// single-event persons that never join the key_* identity. Personless events
// still count in trends; real agent identity starts at the API key.
func (s *MCPServer) baseProps() posthog.Properties {
	props := phProps().Set("transport", s.transport)
	if s.clientIP != "" {
		props = props.Set("$ip", s.clientIP)
	}
	if s.client == nil || strings.TrimSpace(s.client.apiKey) == "" {
		props = props.Set("$process_person_profile", false)
	}
	return props
}
