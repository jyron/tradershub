package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type HTTPMCPServer struct {
	baseURL string
}

func NewHTTPMCPServer(baseURL string) *HTTPMCPServer {
	return &HTTPMCPServer{baseURL: strings.TrimRight(baseURL, "/")}
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
	case "/mcp":
		s.handleMCP(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown endpoint"})
	}
}

func (s *HTTPMCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "MCP endpoint accepts POST requests"})
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
	server := NewMCPServer(NewBotTradeClient(s.baseURL, apiKey))
	resp := server.handle(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
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
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
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
