package apiv1

import (
	"bottrade/database"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	oauthAccessTTL  = time.Hour
	oauthRefreshTTL = 30 * 24 * time.Hour
	oauthCodeTTL    = 10 * time.Minute
)

type oauthClientRegistration struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type oauthAuthRequest struct {
	ID                  string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
}

type oauthProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	Name           string
}

func (h *handlers) mountOAuth(app *fiber.App) {
	app.Get("/.well-known/oauth-authorization-server", h.oauthMetadata)
	app.Post("/oauth/register", h.oauthRegister)
	app.Get("/oauth/authorize", h.oauthAuthorize)
	app.Get("/oauth/login/:provider", h.oauthLogin)
	app.Get("/oauth/callback/:provider", h.oauthCallback)
	app.Post("/oauth/token", h.oauthToken)
}

func (h *handlers) oauthMetadata(c *fiber.Ctx) error {
	issuer := strings.TrimRight(h.AppBaseURL, "/")
	return c.JSON(fiber.Map{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"bottrade:trade"},
	})
}

func (h *handlers) oauthRegister(c *fiber.Ctx) error {
	var req oauthClientRegistration
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_client_metadata"})
	}
	if len(req.RedirectURIs) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "redirect_uris required"})
	}
	for _, raw := range req.RedirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	clientID := "bt_oauth_client_" + uuid.NewString()
	redirectJSON, _ := json.Marshal(req.RedirectURIs)
	if _, err := database.DB.Exec(
		`INSERT INTO oauth_clients (id, name, redirect_uris) VALUES (?1, ?2, ?3)`,
		clientID, name, string(redirectJSON),
	); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "client_registration_failed"})
	}
	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"client_id":                  clientID,
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"token_endpoint_auth_method": "none",
	})
}

func (h *handlers) oauthAuthorize(c *fiber.Ctx) error {
	if c.Query("response_type") != "code" {
		return c.Status(http.StatusBadRequest).SendString("response_type must be code")
	}
	req := oauthAuthRequest{
		ID:                  uuid.NewString(),
		ClientID:            strings.TrimSpace(c.Query("client_id")),
		RedirectURI:         strings.TrimSpace(c.Query("redirect_uri")),
		State:               c.Query("state"),
		Scope:               c.Query("scope", "bottrade:trade"),
		Resource:            c.Query("resource"),
		CodeChallenge:       c.Query("code_challenge"),
		CodeChallengeMethod: c.Query("code_challenge_method"),
	}
	if req.ClientID == "" || req.RedirectURI == "" {
		return c.Status(http.StatusBadRequest).SendString("client_id and redirect_uri are required")
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != "S256" {
		return c.Status(http.StatusBadRequest).SendString("PKCE S256 is required")
	}
	if err := h.validateOAuthClientRedirect(req.ClientID, req.RedirectURI); err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	expiresAt := time.Now().UTC().Add(oauthCodeTTL).Format(time.RFC3339)
	if _, err := database.DB.Exec(
		`INSERT INTO oauth_auth_requests
		   (id, client_id, redirect_uri, state, scope, resource, code_challenge, code_challenge_method, expires_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
		req.ID, req.ClientID, req.RedirectURI, req.State, req.Scope, req.Resource,
		req.CodeChallenge, req.CodeChallengeMethod, expiresAt,
	); err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to start authorization")
	}
	return c.Type("html").SendString(h.renderOAuthLogin(req.ID))
}

func (h *handlers) oauthLogin(c *fiber.Ctx) error {
	provider := c.Params("provider")
	requestID := c.Query("request_id")
	if requestID == "" {
		return c.Status(http.StatusBadRequest).SendString("request_id required")
	}
	state, err := randomOAuthToken("bt_state_")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create state")
	}
	state = requestID + "." + state
	c.Cookie(&fiber.Cookie{
		Name:     "bt_oauth_state",
		Value:    state,
		Path:     "/",
		HTTPOnly: true,
		Secure:   strings.HasPrefix(h.AppBaseURL, "https://"),
		SameSite: "Lax",
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
	authURL, err := h.providerAuthURL(provider, state)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect(authURL, http.StatusFound)
}

func (h *handlers) oauthCallback(c *fiber.Ctx) error {
	provider := c.Params("provider")
	state := c.Query("state")
	if state == "" || state != c.Cookies("bt_oauth_state") {
		return c.Status(http.StatusBadRequest).SendString("invalid oauth state")
	}
	requestID := strings.SplitN(state, ".", 2)[0]
	authReq, err := loadOAuthAuthRequest(requestID)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString("authorization request expired")
	}
	profile, err := h.fetchOAuthProfile(provider, c.Query("code"))
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	accountID, err := upsertOAuthAccount(profile)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account")
	}
	if _, err := ensureAccountAPIKey(accountID, profile.Name, profile.Email); err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account key")
	}
	code, err := randomOAuthToken("bt_code_")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create code")
	}
	if _, err := database.DB.Exec(
		`INSERT INTO oauth_auth_codes
		   (code_hash, account_id, client_id, redirect_uri, scope, resource,
		    code_challenge, code_challenge_method, expires_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`,
		hashToken(code), accountID, authReq.ClientID, authReq.RedirectURI, authReq.Scope, authReq.Resource,
		authReq.CodeChallenge, authReq.CodeChallengeMethod, time.Now().UTC().Add(oauthCodeTTL).Format(time.RFC3339),
	); err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create code")
	}
	_, _ = database.DB.Exec(`DELETE FROM oauth_auth_requests WHERE id = ?1`, requestID)
	redirectTo, _ := url.Parse(authReq.RedirectURI)
	q := redirectTo.Query()
	q.Set("code", code)
	if authReq.State != "" {
		q.Set("state", authReq.State)
	}
	redirectTo.RawQuery = q.Encode()
	return c.Redirect(redirectTo.String(), http.StatusFound)
}

func (h *handlers) oauthToken(c *fiber.Ctx) error {
	grantType := oauthParam(c, "grant_type")
	switch grantType {
	case "authorization_code":
		return h.oauthAuthorizationCodeToken(c)
	case "refresh_token":
		return h.oauthRefreshToken(c)
	default:
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "unsupported_grant_type"})
	}
}

func (h *handlers) oauthAuthorizationCodeToken(c *fiber.Ctx) error {
	code := oauthParam(c, "code")
	clientID := oauthParam(c, "client_id")
	redirectURI := oauthParam(c, "redirect_uri")
	codeVerifier := oauthParam(c, "code_verifier")
	if code == "" || clientID == "" || redirectURI == "" || codeVerifier == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	var accountID, storedClientID, storedRedirect, scope, resource, challenge, method, expiresAt, usedAt string
	err := database.DB.QueryRow(
		`SELECT account_id, client_id, redirect_uri, scope, resource,
		        code_challenge, code_challenge_method, expires_at, COALESCE(used_at, '')
		   FROM oauth_auth_codes
		  WHERE code_hash = ?1`,
		hashToken(code),
	).Scan(&accountID, &storedClientID, &storedRedirect, &scope, &resource, &challenge, &method, &expiresAt, &usedAt)
	if err != nil || usedAt != "" || storedClientID != clientID || storedRedirect != redirectURI {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_grant"})
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().UTC().After(exp) || method != "S256" || pkceS256(codeVerifier) != challenge {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_grant"})
	}
	_, _ = database.DB.Exec(`UPDATE oauth_auth_codes SET used_at = CURRENT_TIMESTAMP WHERE code_hash = ?1`, hashToken(code))
	return issueOAuthTokens(c, accountID, clientID, scope, resource)
}

func (h *handlers) oauthRefreshToken(c *fiber.Ctx) error {
	refreshToken := oauthParam(c, "refresh_token")
	clientID := oauthParam(c, "client_id")
	if refreshToken == "" || clientID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_request"})
	}
	var accountID, storedClientID, scope, resource, expiresAt, revokedAt string
	err := database.DB.QueryRow(
		`SELECT account_id, client_id, scope, resource, expires_at, COALESCE(revoked_at, '')
		   FROM oauth_refresh_tokens
		  WHERE token_hash = ?1`,
		hashToken(refreshToken),
	).Scan(&accountID, &storedClientID, &scope, &resource, &expiresAt, &revokedAt)
	if err != nil || revokedAt != "" || storedClientID != clientID {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_grant"})
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().UTC().After(exp) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid_grant"})
	}
	return issueOAuthTokens(c, accountID, clientID, scope, resource)
}

func issueOAuthTokens(c *fiber.Ctx, accountID, clientID, scope, resource string) error {
	accessToken, err := randomOAuthToken("bt_oat_")
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "server_error"})
	}
	refreshToken, err := randomOAuthToken("bt_ort_")
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "server_error"})
	}
	now := time.Now().UTC()
	if _, err := database.DB.Exec(
		`INSERT INTO oauth_access_tokens
		   (token_hash, account_id, client_id, scope, resource, expires_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
		hashToken(accessToken), accountID, clientID, scope, resource, now.Add(oauthAccessTTL).Format(time.RFC3339),
	); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "server_error"})
	}
	if _, err := database.DB.Exec(
		`INSERT INTO oauth_refresh_tokens
		   (token_hash, account_id, client_id, scope, resource, expires_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6)`,
		hashToken(refreshToken), accountID, clientID, scope, resource, now.Add(oauthRefreshTTL).Format(time.RFC3339),
	); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "server_error"})
	}
	return c.JSON(fiber.Map{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(oauthAccessTTL.Seconds()),
		"scope":         scope,
	})
}

func (h *handlers) providerAuthURL(provider, state string) (string, error) {
	callback := strings.TrimRight(h.AppBaseURL, "/") + "/oauth/callback/" + provider
	switch provider {
	case "google":
		if h.GoogleClientID == "" || h.GoogleClientSecret == "" {
			return "", fmt.Errorf("Google login is not configured")
		}
		v := url.Values{}
		v.Set("client_id", h.GoogleClientID)
		v.Set("redirect_uri", callback)
		v.Set("response_type", "code")
		v.Set("scope", "openid email profile")
		v.Set("state", state)
		return "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode(), nil
	case "github":
		if h.GitHubClientID == "" || h.GitHubClientSecret == "" {
			return "", fmt.Errorf("GitHub login is not configured")
		}
		v := url.Values{}
		v.Set("client_id", h.GitHubClientID)
		v.Set("redirect_uri", callback)
		v.Set("scope", "read:user user:email")
		v.Set("state", state)
		return "https://github.com/login/oauth/authorize?" + v.Encode(), nil
	default:
		return "", fmt.Errorf("unknown provider")
	}
}

func (h *handlers) fetchOAuthProfile(provider, code string) (oauthProfile, error) {
	if code == "" {
		return oauthProfile{}, fmt.Errorf("missing provider code")
	}
	switch provider {
	case "google":
		return h.fetchGoogleProfile(code)
	case "github":
		return h.fetchGitHubProfile(code)
	default:
		return oauthProfile{}, fmt.Errorf("unknown provider")
	}
}

func (h *handlers) fetchGoogleProfile(code string) (oauthProfile, error) {
	token, err := exchangeProviderCode("https://oauth2.googleapis.com/token", url.Values{
		"client_id":     {h.GoogleClientID},
		"client_secret": {h.GoogleClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {strings.TrimRight(h.AppBaseURL, "/") + "/oauth/callback/google"},
	})
	if err != nil {
		return oauthProfile{}, err
	}
	var profile struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := getProviderJSON("https://openidconnect.googleapis.com/v1/userinfo", token, &profile); err != nil {
		return oauthProfile{}, err
	}
	return oauthProfile{Provider: "google", ProviderUserID: profile.Sub, Email: profile.Email, Name: profile.Name}, nil
}

func (h *handlers) fetchGitHubProfile(code string) (oauthProfile, error) {
	token, err := exchangeProviderCode("https://github.com/login/oauth/access_token", url.Values{
		"client_id":     {h.GitHubClientID},
		"client_secret": {h.GitHubClientSecret},
		"code":          {code},
		"redirect_uri":  {strings.TrimRight(h.AppBaseURL, "/") + "/oauth/callback/github"},
	})
	if err != nil {
		return oauthProfile{}, err
	}
	var profile struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := getProviderJSON("https://api.github.com/user", token, &profile); err != nil {
		return oauthProfile{}, err
	}
	email := profile.Email
	if email == "" {
		var emails []struct {
			Email   string `json:"email"`
			Primary bool   `json:"primary"`
		}
		if err := getProviderJSON("https://api.github.com/user/emails", token, &emails); err == nil {
			for _, e := range emails {
				if e.Primary {
					email = e.Email
					break
				}
			}
		}
	}
	name := profile.Name
	if name == "" {
		name = profile.Login
	}
	return oauthProfile{Provider: "github", ProviderUserID: fmt.Sprintf("%d", profile.ID), Email: email, Name: name}, nil
}

func exchangeProviderCode(endpoint string, values url.Values) (string, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		if out.Error == "" {
			out.Error = "provider token exchange failed"
		}
		return "", fmt.Errorf("%s", out.Error)
	}
	return out.AccessToken, nil
}

func getProviderJSON(endpoint, token string, out any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("provider profile failed: %s", strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func upsertOAuthAccount(profile oauthProfile) (string, error) {
	var accountID string
	err := database.DB.QueryRow(
		`SELECT account_id FROM account_identities WHERE provider = ?1 AND provider_user_id = ?2`,
		profile.Provider, profile.ProviderUserID,
	).Scan(&accountID)
	if err == nil {
		return accountID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	accountID = uuid.NewString()
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = profile.Provider + " user"
	}
	tx, err := database.DB.Begin()
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.Exec(
		`INSERT INTO accounts (id, name, email, billing_email, plan) VALUES (?1, ?2, ?3, ?3, 'free')`,
		accountID, name, profile.Email,
	)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(
		`INSERT INTO account_identities (account_id, provider, provider_user_id, email, name)
		 VALUES (?1, ?2, ?3, ?4, ?5)`,
		accountID, profile.Provider, profile.ProviderUserID, profile.Email, name,
	)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return accountID, nil
}

func loadOAuthAuthRequest(id string) (oauthAuthRequest, error) {
	var req oauthAuthRequest
	var expiresAt string
	err := database.DB.QueryRow(
		`SELECT id, client_id, redirect_uri, state, scope, resource,
		        code_challenge, code_challenge_method, expires_at
		   FROM oauth_auth_requests
		  WHERE id = ?1`,
		id,
	).Scan(&req.ID, &req.ClientID, &req.RedirectURI, &req.State, &req.Scope, &req.Resource,
		&req.CodeChallenge, &req.CodeChallengeMethod, &expiresAt)
	if err != nil {
		return req, err
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().UTC().After(exp) {
		return req, sql.ErrNoRows
	}
	return req, nil
}

func (h *handlers) validateOAuthClientRedirect(clientID, redirectURI string) error {
	if err := validateRedirectURI(redirectURI); err != nil {
		return err
	}
	var redirectJSON string
	if err := database.DB.QueryRow(
		`SELECT redirect_uris FROM oauth_clients WHERE id = ?1`,
		clientID,
	).Scan(&redirectJSON); err != nil {
		return fmt.Errorf("unknown client_id")
	}
	var allowed []string
	if err := json.Unmarshal([]byte(redirectJSON), &allowed); err != nil {
		return fmt.Errorf("invalid client metadata")
	}
	for _, candidate := range allowed {
		if candidate == redirectURI {
			return nil
		}
	}
	return fmt.Errorf("redirect_uri is not registered")
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid redirect_uri")
	}
	if u.Scheme != "https" && !strings.HasPrefix(u.Host, "localhost") && !strings.HasPrefix(u.Host, "127.0.0.1") {
		return fmt.Errorf("redirect_uri must be https")
	}
	return nil
}

func (h *handlers) renderOAuthLogin(requestID string) string {
	google := "/oauth/login/google?request_id=" + url.QueryEscape(requestID)
	github := "/oauth/login/github?request_id=" + url.QueryEscape(requestID)
	return `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Authorize BotTrade</title></head>
<body style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#0b0d10;color:#fff;">
  <main style="width:min(420px,calc(100% - 32px));border:1px solid rgba(255,255,255,.16);border-radius:10px;padding:28px;background:#11151a;">
    <h1 style="margin:0 0 10px;font-size:26px;">Authorize BotTrade</h1>
    <p style="line-height:1.5;color:rgba(255,255,255,.72);margin:0 0 20px;">Sign in to connect your BotTrade account. Usage, runs, billing, and leaderboard identity stay attached to that account.</p>
    <a href="` + html.EscapeString(google) + `" style="display:block;text-align:center;padding:12px 14px;margin-bottom:10px;background:#fff;color:#111;border-radius:6px;text-decoration:none;font-weight:700;">Continue with Google</a>
    <a href="` + html.EscapeString(github) + `" style="display:block;text-align:center;padding:12px 14px;background:#24292f;color:#fff;border-radius:6px;text-decoration:none;font-weight:700;">Continue with GitHub</a>
  </main>
</body>
</html>`
}

func oauthParam(c *fiber.Ctx, key string) string {
	if v := c.FormValue(key); v != "" {
		return strings.TrimSpace(v)
	}
	var body map[string]string
	if err := c.BodyParser(&body); err == nil {
		return strings.TrimSpace(body[key])
	}
	return ""
}

func randomOAuthToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
