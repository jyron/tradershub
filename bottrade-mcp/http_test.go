package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPMCPToolList(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("MCP-Protocol-Version"); got != protocolVersion {
		t.Fatalf("MCP-Protocol-Version = %q", got)
	}
	var resp struct {
		Result struct {
			Tools []tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !hasTool(resp.Result.Tools, "scan_market") {
		t.Fatal("expected scan_market tool")
	}
	if !hasTool(resp.Result.Tools, "connect_bottrade") {
		t.Fatal("expected connect_bottrade tool")
	}
}

func TestHTTPMCPConnectReturnsAuthorizeURL(t *testing.T) {
	t.Setenv("BOTTRADE_MCP_OAUTH_CLIENT_ID", "test-client")
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"connect_bottrade","arguments":{}}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	loginURL, _ := resp.Result.StructuredContent.(map[string]any)["login_url"].(string)
	if !strings.Contains(loginURL, "/oauth/authorize") {
		t.Fatalf("login_url = %#v", loginURL)
	}
	if rec.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("missing Mcp-Session-Id")
	}
}

func TestHTTPMCPProtectedToolRequiresAuth(t *testing.T) {
	t.Setenv("BOTTRADE_MCP_OAUTH_CLIENT_ID", "test-client")
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"start_run","arguments":{"scenario_slug":"sandbox-nov-2024"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	auth := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(auth, `resource_metadata="https://mcp.bot-trade.org/.well-known/oauth-protected-resource"`) {
		t.Fatalf("WWW-Authenticate = %q", auth)
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyMap); err != nil {
		t.Fatal(err)
	}
	if bodyMap["error"] != "auth_required" {
		t.Fatalf("error = %#v", bodyMap["error"])
	}
	if !strings.Contains(bodyMap["login_url"].(string), "/oauth/authorize") {
		t.Fatalf("login_url = %#v", bodyMap["login_url"])
	}
	if rec.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("missing Mcp-Session-Id")
	}
}

func TestHTTPMCPInitializeCreatesSession(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("missing Mcp-Session-Id")
	}
}

func TestHTTPMCPGetExplainsEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyMap); err != nil {
		t.Fatal(err)
	}
	if bodyMap["error"] != "POST required" {
		t.Fatalf("error = %#v", bodyMap["error"])
	}
}

func TestHTTPMCPNotificationAccepted(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHTTPMCPServer("https://bot-trade.org").ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeyFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer bt_test")
	if got := apiKeyFromRequest(req); got != "bt_test" {
		t.Fatalf("bearer key = %q", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer bt_wrong")
	req.Header.Set("X-API-Key", "bt_right")
	if got := apiKeyFromRequest(req); got != "bt_right" {
		t.Fatalf("x api key = %q", got)
	}
}
