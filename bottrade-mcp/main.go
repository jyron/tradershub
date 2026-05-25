package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	baseURL := strings.TrimSpace(os.Getenv("BOTTRADE_API_BASE"))
	if baseURL == "" {
		baseURL = "https://bot-trade.org"
	}

	transport := strings.TrimSpace(os.Getenv("BOTTRADE_MCP_TRANSPORT"))
	port := strings.TrimSpace(os.Getenv("PORT"))
	if transport == "" && port != "" {
		transport = "http"
	}
	if transport == "http" {
		addr := strings.TrimSpace(os.Getenv("BOTTRADE_MCP_ADDR"))
		if addr == "" {
			if port == "" {
				port = "8080"
			}
			addr = ":" + port
		}
		log.Printf("bottrade-mcp listening on %s", addr)
		if err := runHTTP(context.Background(), addr, baseURL); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("bottrade-mcp: %v", err)
		}
		return
	}

	client := NewBotTradeClient(baseURL, strings.TrimSpace(os.Getenv("BOTTRADE_API_KEY")))
	server := NewMCPServer(client)
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatalf("bottrade-mcp: %v", err)
	}
}
