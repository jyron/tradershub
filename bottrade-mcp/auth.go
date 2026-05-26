package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const authSessionTTL = 30 * time.Minute

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*MCPSession
}

type MCPSession struct {
	ID           string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	State        string
	Verifier     string
	LoginURL     string
	LoginShown   bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: map[string]*MCPSession{}}
}

func (s *SessionStore) GetOrCreate(id string) *MCPSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if id != "" {
		if sess := s.sessions[id]; sess != nil {
			if now.Sub(sess.UpdatedAt) > authSessionTTL {
				delete(s.sessions, id)
			} else {
				sess.UpdatedAt = now
				return sess
			}
		}
	}
	if id == "" {
		id = "bt_mcp_" + randomURLToken(24)
	}
	sess := &MCPSession{ID: id, CreatedAt: now, UpdatedAt: now}
	s.sessions[id] = sess
	return sess
}

func (s *SessionStore) ByState(state string) *MCPSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, sess := range s.sessions {
		if now.Sub(sess.UpdatedAt) > authSessionTTL {
			delete(s.sessions, sess.ID)
			continue
		}
		if sess.State == state {
			return sess
		}
	}
	return nil
}

func (s *SessionStore) SetPending(id, state, verifier, loginURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		sess.State = state
		sess.Verifier = verifier
		sess.LoginURL = loginURL
		sess.LoginShown = false
		sess.UpdatedAt = time.Now().UTC()
	}
}

func (s *SessionStore) MarkLoginShown(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		sess.LoginShown = true
		sess.UpdatedAt = time.Now().UTC()
	}
}

func (s *SessionStore) SetToken(id, accessToken, refreshToken string, expiresIn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		sess.AccessToken = accessToken
		sess.RefreshToken = refreshToken
		sess.ExpiresAt = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
		sess.State = ""
		sess.Verifier = ""
		sess.LoginURL = ""
		sess.LoginShown = false
		sess.UpdatedAt = time.Now().UTC()
	}
}

func (s *MCPSession) ValidToken() string {
	if s == nil || s.AccessToken == "" || time.Now().UTC().After(s.ExpiresAt.Add(-30*time.Second)) {
		return ""
	}
	return s.AccessToken
}

type OAuthBridge struct {
	apiBase   string
	publicURL string
	store     *SessionStore
	client    *http.Client

	mu       sync.Mutex
	clientID string
}

func NewOAuthBridge(apiBase, publicURL string, store *SessionStore) *OAuthBridge {
	return &OAuthBridge{
		apiBase:   strings.TrimRight(apiBase, "/"),
		publicURL: strings.TrimRight(publicURL, "/"),
		store:     store,
		client:    &http.Client{Timeout: 20 * time.Second},
		clientID:  strings.TrimSpace(os.Getenv("BOTTRADE_MCP_OAUTH_CLIENT_ID")),
	}
}

func (o *OAuthBridge) Start(ctx context.Context, sess *MCPSession) (map[string]any, error) {
	if token := sess.ValidToken(); token != "" {
		return map[string]any{"status": "connected"}, nil
	}
	if sess.LoginURL != "" {
		return map[string]any{"status": "pending", "login_url": sess.LoginURL}, nil
	}
	clientID, err := o.oauthClientID(ctx)
	if err != nil {
		return nil, err
	}
	state := "bt_state_" + randomURLToken(24)
	verifier := "bt_verifier_" + randomURLToken(48)
	redirectURI := o.publicURL + "/oauth/callback"
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "bottrade:trade")
	q.Set("resource", o.publicURL+"/mcp")
	q.Set("state", state)
	q.Set("code_challenge", pkceS256(verifier))
	q.Set("code_challenge_method", "S256")
	loginURL := o.apiBase + "/oauth/authorize?" + q.Encode()
	o.store.SetPending(sess.ID, state, verifier, loginURL)
	return map[string]any{"status": "pending", "login_url": loginURL}, nil
}

func (o *OAuthBridge) Complete(ctx context.Context, code, state string) error {
	sess := o.store.ByState(state)
	if sess == nil {
		return fmt.Errorf("invalid session")
	}
	clientID, err := o.oauthClientID(ctx)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("redirect_uri", o.publicURL+"/oauth/callback")
	form.Set("code_verifier", sess.Verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.apiBase+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("token exchange failed: %s", strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("missing access token")
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	o.store.SetToken(sess.ID, tok.AccessToken, tok.RefreshToken, tok.ExpiresIn)
	return nil
}

func (o *OAuthBridge) oauthClientID(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.clientID != "" {
		return o.clientID, nil
	}
	body, _ := json.Marshal(map[string]any{
		"client_name":   "BotTrade MCP",
		"redirect_uris": []string{o.publicURL + "/oauth/callback"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.apiBase+"/oauth/register", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("oauth registration failed: %s", strings.TrimSpace(string(respBody)))
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if out.ClientID == "" {
		return "", fmt.Errorf("oauth registration returned no client_id")
	}
	o.clientID = out.ClientID
	return o.clientID, nil
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomURLToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
