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
	req.Header.Set("Authorization", "Bearer bt_test")
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
}

func TestHTTPMCPToolListRequiresAuth(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
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
	if bodyMap["error"] != "authorization_required" {
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
