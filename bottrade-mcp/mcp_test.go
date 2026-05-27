package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPHandshakeAndToolList(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n") + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), out.String())
	}

	var initResp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatal(err)
	}
	result := initResp["result"].(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}

	var listResp struct {
		Result struct {
			Tools []tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}
	if !hasTool(listResp.Result.Tools, "auth_status") {
		t.Fatal("expected auth_status tool")
	}
	if !hasTool(listResp.Result.Tools, "list_scenarios") {
		t.Fatal("expected list_scenarios tool")
	}
	if !hasTool(listResp.Result.Tools, "scan_market") {
		t.Fatal("expected scan_market tool")
	}
	if !hasTool(listResp.Result.Tools, "inspect_symbols") {
		t.Fatal("expected inspect_symbols tool")
	}
	if !hasTool(listResp.Result.Tools, "submit_decision") {
		t.Fatal("expected submit_decision tool")
	}
	for _, name := range []string{"advance_until_next_session", "hold_until_end", "liquidate_and_finish", "run_sandbox_smoke_test", "get_trades"} {
		if !hasTool(listResp.Result.Tools, name) {
			t.Fatalf("expected %s tool", name)
		}
	}
	var scan tool
	for _, candidate := range listResp.Result.Tools {
		if candidate.Name == "scan_market" {
			scan = candidate
			break
		}
	}
	if scan.Annotations["readOnlyHint"] != true {
		t.Fatalf("scan_market readOnlyHint = %#v, want true", scan.Annotations["readOnlyHint"])
	}
}

func TestProtectedToolRequiresAPIKey(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_run","arguments":{"run_id":"run_123"}}}` + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Result.IsError {
		t.Fatal("expected tool error")
	}
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "auth required") {
		t.Fatalf("unexpected error text: %q", got)
	}
	structured, ok := resp.Result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", resp.Result.StructuredContent)
	}
	if structured["next_action"] != "connect_bottrade" {
		t.Fatalf("next_action = %#v", structured["next_action"])
	}
	if structured["status"] != "auth_required" {
		t.Fatalf("status = %#v", structured["status"])
	}
}

func TestAuthStatusWithoutAPIKeyIsStructured(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"auth_status","arguments":{}}}` + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Result.IsError {
		t.Fatalf("unexpected tool error: %#v", resp.Result.Content)
	}
	structured, ok := resp.Result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %#v", resp.Result.StructuredContent)
	}
	if structured["status"] != "auth_required" {
		t.Fatalf("status = %#v", structured["status"])
	}
	if structured["next_action"] != "connect_bottrade" {
		t.Fatalf("next_action = %#v", structured["next_action"])
	}
}

func TestRawMarketGuardrails(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_market","arguments":{"run_id":"run_123","lookback":50}}}` + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Result.IsError {
		t.Fatal("expected tool error")
	}
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "scan_market") {
		t.Fatalf("unexpected error text: %q", got)
	}
}

func TestStepRunRejectsBatchStepping(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"step_run","arguments":{"run_id":"run_123","count":200}}}` + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Result.IsError {
		t.Fatal("expected tool error")
	}
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "count=1") {
		t.Fatalf("unexpected error text: %q", got)
	}
}

func TestSubmitDecisionRejectsBatchStepping(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"submit_decision","arguments":{"run_id":"run_123","action":"hold","orders":[],"step_count":200}}}` + "\n"

	server := NewMCPServer(NewBotTradeClient("https://bot-trade.org", ""))
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Result.IsError {
		t.Fatal("expected tool error")
	}
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "count=1") {
		t.Fatalf("unexpected error text: %q", got)
	}
}

func hasTool(tools []tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
