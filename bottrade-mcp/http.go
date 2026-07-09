package main

import (
	"net"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type HTTPMCPServer struct {
	baseURL   string
	publicURL string
	sessions  *SessionStore
	auth      *OAuthBridge
	analytics *Analytics
}

func NewHTTPMCPServer(baseURL string) *HTTPMCPServer {
	publicURL := strings.TrimSpace(os.Getenv("BOTTRADE_MCP_PUBLIC_URL"))
	if publicURL == "" {
		publicURL = "https://mcp.bot-trade.org"
	}
	sessions := NewSessionStore()
	server := &HTTPMCPServer{
		baseURL:   strings.TrimRight(baseURL, "/"),
		publicURL: strings.TrimRight(publicURL, "/"),
		sessions:  sessions,
	}
	server.auth = NewOAuthBridge(server.baseURL, server.publicURL, sessions)
	return server
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
	case "/oauth/callback":
		s.handleOAuthCallback(w, r)
	case "/mcp":
		s.handleMCP(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/connect/") {
			s.handleConnect(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown endpoint"})
	}
}

func (s *HTTPMCPServer) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET required"})
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
			"error": "POST required",
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
	session := s.sessions.GetOrCreate(r.Header.Get("Mcp-Session-Id"))
	w.Header().Set("Mcp-Session-Id", session.ID)
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	apiKey := apiKeyFromRequest(r)
	if apiKey == "" {
		apiKey = session.ValidToken()
	}
	if apiKey == "" && mcpRequestRequiresAuth(req) {
		loginURL := session.LoginURL
		if status, err := s.auth.Start(r.Context(), session); err == nil {
			if url, _ := status["login_url"].(string); url != "" {
				loginURL = url
			}
		}
		writeMCPAuthRequired(w, s.publicURL, loginURL)
		return
	}
	server := NewMCPServerWithAuth(NewBotTradeClient(s.baseURL, apiKey), session, s.auth)
	server.analytics = s.analytics
	server.transport = "http"
	server.clientIP = realClientIP(r)
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
	case "auth_status", "connect_bottrade", "list_scenarios", "get_scenario":
		return true
	default:
		return false
	}
}

func (s *HTTPMCPServer) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET required"})
		return
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		writeHTML(w, http.StatusBadRequest, "BotTrade auth failed: "+errMsg)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		writeHTML(w, http.StatusBadRequest, "Missing auth code.")
		return
	}
	if err := s.auth.Complete(r.Context(), code, state); err != nil {
		writeHTML(w, http.StatusBadRequest, "BotTrade auth failed.")
		return
	}
	writeHTML(w, http.StatusOK, "BotTrade connected. Return to your agent.")
}

func (s *HTTPMCPServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET required"})
		return
	}
	id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/connect/"))
	if err != nil || id == "" {
		writeHTML(w, http.StatusBadRequest, "Invalid BotTrade connect link.")
		return
	}
	session := s.sessions.Get(id)
	if session == nil {
		writeHTML(w, http.StatusBadRequest, "BotTrade connect link expired. Ask the agent to connect again.")
		return
	}
	if session.ValidToken() != "" {
		writeHTML(w, http.StatusOK, "BotTrade connected. Return to your agent.")
		return
	}
	if session.LoginURL == "" {
		writeHTML(w, http.StatusBadRequest, "BotTrade connect link expired. Ask the agent to connect again.")
		return
	}
	http.Redirect(w, r, session.LoginURL, http.StatusFound)
}

func writeMCPAuthRequired(w http.ResponseWriter, publicURL, loginURL string) {
	metadataURL := strings.TrimRight(publicURL, "/") + "/.well-known/oauth-protected-resource"
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`", scope="bottrade:trade"`)
	if loginURL == "" {
		loginURL = "https://bot-trade.org/login"
	}
	body := authRequiredPayload(loginURL)
	body["resource_metadata"] = metadataURL
	writeJSON(w, http.StatusUnauthorized, body)
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

func writeHTML(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><title>BotTrade</title><body style="font-family:system-ui,sans-serif;padding:40px;line-height:1.4"><h1 style="font-size:24px">` + html.EscapeString(text) + `</h1></body>`))
}

func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	writeJSON(w, http.StatusOK, response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func runHTTP(ctx context.Context, addr, baseURL string, an *Analytics) error {
	handler := NewHTTPMCPServer(baseURL)
	handler.analytics = an
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}

// realClientIP resolves the caller's IP behind the Cloudflare/Railway edge.
func realClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
