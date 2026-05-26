package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

type HTTPMCPServer struct {
	baseURL   string
	publicURL string
}

func NewHTTPMCPServer(baseURL string) *HTTPMCPServer {
	publicURL := strings.TrimSpace(os.Getenv("BOTTRADE_MCP_PUBLIC_URL"))
	if publicURL == "" {
		publicURL = "https://mcp.bot-trade.org"
	}
	return &HTTPMCPServer{
		baseURL:   strings.TrimRight(baseURL, "/"),
		publicURL: strings.TrimRight(publicURL, "/"),
	}
}

func (s *HTTPMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setMCPHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.URL.Path {
	case "/health":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "/.well-known/oauth-protected-resource":
		s.handleProtectedResourceMetadata(w, r)
	case "/mcp":
		s.handleMCP(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown endpoint"})
	}
}

func (s *HTTPMCPServer) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "metadata endpoint accepts GET requests"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.publicURL + "/mcp",
		"authorization_servers":    []string{"https://bot-trade.org"},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"bottrade:trade"},
	})
}

func (s *HTTPMCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error":       "This is the BotTrade MCP endpoint for agent clients.",
			"mcp_url":     s.publicURL + "/mcp",
			"login_url":   "https://bot-trade.org/login",
			"account_url": "https://bot-trade.org/account",
			"hint":        "Add the MCP URL inside an MCP client. Use the login URL in a browser.",
		})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8*1024*1024))
	if err != nil {
		writeRPCError(w, nil, -32700, err.Error())
		return
	}
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, err.Error())
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	apiKey := apiKeyFromRequest(r)
	if apiKey == "" && mcpRequestRequiresAuth(req) {
		writeMCPAuthRequired(w, s.publicURL)
		return
	}
	server := NewMCPServer(NewBotTradeClient(s.baseURL, apiKey))
	resp := server.handle(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

func mcpRequestRequiresAuth(req request) bool {
	switch req.Method {
	case "initialize", "ping", "tools/list", "resources/list", "resources/read", "prompts/list", "prompts/get":
		return false
	case "tools/call":
		name := toolNameFromParams(req.Params)
		return !isPublicTool(name)
	default:
		return true
	}
}

func toolNameFromParams(raw json.RawMessage) string {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return p.Name
}

func isPublicTool(name string) bool {
	switch name {
	case "connect_bottrade", "list_scenarios", "get_scenario":
		return true
	default:
		return false
	}
}

func writeMCPAuthRequired(w http.ResponseWriter, publicURL string) {
	metadataURL := strings.TrimRight(publicURL, "/") + "/.well-known/oauth-protected-resource"
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`", scope="bottrade:trade"`)
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error":                 "authorization_required",
		"authorization_server":  "https://bot-trade.org",
		"resource_metadata_url": metadataURL,
		"login_url":             "https://bot-trade.org/login",
	})
}

func apiKeyFromRequest(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "Bearer "
	if strings.HasPrefix(auth, bearer) {
		return strings.TrimSpace(strings.TrimPrefix(auth, bearer))
	}
	return ""
}

func setMCPHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version, Mcp-Session-Id, X-API-Key")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
	w.Header().Set("MCP-Protocol-Version", protocolVersion)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	writeJSON(w, http.StatusOK, response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func runHTTP(ctx context.Context, addr, baseURL string) error {
	server := &http.Server{
		Addr:    addr,
		Handler: NewHTTPMCPServer(baseURL),
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}
