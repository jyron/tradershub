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
	if listResp.Result.Tools[0].Name != "connect_bottrade" {
		t.Fatalf("first tool = %q", listResp.Result.Tools[0].Name)
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
	if got := resp.Result.Content[0].Text; !strings.Contains(got, "authentication is required") {
		t.Fatalf("unexpected error text: %q", got)
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

func hasTool(tools []tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
