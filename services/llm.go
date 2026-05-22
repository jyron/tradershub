package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Minimal Anthropic Messages client used by the daily-recap job. Kept here
// (not as a wrapper over the python SDK) because Go-side calls happen on a
// timer, not in response to a request, so the python subprocess dance from
// the bot path would be wasteful.

var (
	anthropicKey   string
	anthropicMutex sync.RWMutex
)

func SetAnthropicAPIKey(k string) {
	anthropicMutex.Lock()
	defer anthropicMutex.Unlock()
	anthropicKey = k
}

func AnthropicKey() string {
	anthropicMutex.RLock()
	defer anthropicMutex.RUnlock()
	return anthropicKey
}

const anthropicMessagesURL = "https://api.anthropic.com/v1/messages"

type anthropicReq struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type anthropicResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Type       string `json:"type"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// AnthropicComplete returns the assistant text for a single-turn request.
// Returns ErrNoAnthropicKey if no key is configured — callers should fall back.
var ErrNoAnthropicKey = errors.New("ANTHROPIC_API_KEY not configured")

func AnthropicComplete(ctx context.Context, model, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	key := AnthropicKey()
	if key == "" {
		return "", ErrNoAnthropicKey
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body, err := json.Marshal(anthropicReq{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(raw))
	}
	var parsed anthropicResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic error: %s — %s", parsed.Error.Type, parsed.Error.Message)
	}
	for _, c := range parsed.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("anthropic returned no text content (stop=%s)", parsed.StopReason)
}
