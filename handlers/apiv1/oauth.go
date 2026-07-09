package apiv1

import (
	"bottrade/analytics"
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
	siteSessionTTL  = 30 * 24 * time.Hour
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

	app.Get("/login", h.siteLogin)
	app.Get("/auth/login/:provider", h.siteProviderLogin)
	app.Get("/auth/callback/:provider", h.siteProviderCallback)
	app.Get("/account", h.siteAccount)
	app.Get("/account/data", h.siteAccountData)
	app.Post("/logout", h.siteLogout)
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
	authURL, err := h.providerAuthURL(provider, state, "/auth/callback/"+provider)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect(authURL, http.StatusFound)
}

func (h *handlers) siteLogin(c *fiber.Ctx) error {
	if accountID, _ := h.siteSessionAccountID(c); accountID != "" {
		return c.Redirect("/account", http.StatusFound)
	}
	return c.Type("html").SendString(h.renderSiteLogin())
}

func (h *handlers) siteProviderLogin(c *fiber.Ctx) error {
	provider := c.Params("provider")
	state, err := randomOAuthToken("bt_site_state_")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create state")
	}
	c.Cookie(&fiber.Cookie{
		Name:     "bt_site_state",
		Value:    state,
		Path:     "/",
		HTTPOnly: true,
		Secure:   strings.HasPrefix(h.AppBaseURL, "https://"),
		SameSite: "Lax",
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
	authURL, err := h.providerAuthURL(provider, state, "/auth/callback/"+provider)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect(authURL, http.StatusFound)
}

func (h *handlers) siteProviderCallback(c *fiber.Ctx) error {
	provider := c.Params("provider")
	state := c.Query("state")
	if state != "" && state == c.Cookies("bt_oauth_state") {
		return h.completeOAuthProviderCallback(c, provider, "/auth/callback/"+provider)
	}
	if state == "" || state != c.Cookies("bt_site_state") {
		return c.Status(http.StatusBadRequest).SendString("invalid login state")
	}
	profile, err := h.fetchOAuthProfile(provider, c.Query("code"), "/auth/callback/"+provider)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	accountID, created, err := upsertOAuthAccount(profile)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account")
	}
	key, keyCreated, err := ensureAccountAPIKey(accountID, profile.Name, profile.Email)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account key")
	}
	h.captureAuth(c, accountID, created, profile, "site")
	if keyCreated {
		h.Analytics.Capture(accountID, "api_key_issued", analytics.Props().
			Set("plan", key.Plan).Set("flow", "site"))
	}
	sessionToken, err := h.createSiteSession(accountID)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create session")
	}
	c.Cookie(&fiber.Cookie{
		Name:     "bt_session",
		Value:    sessionToken,
		Path:     "/",
		HTTPOnly: true,
		Secure:   strings.HasPrefix(h.AppBaseURL, "https://"),
		SameSite: "Lax",
		MaxAge:   int(siteSessionTTL.Seconds()),
	})
	return c.Redirect("/account", http.StatusFound)
}

func (h *handlers) siteAccount(c *fiber.Ctx) error {
	accountID, err := h.siteSessionAccountID(c)
	if err != nil || accountID == "" {
		return c.Redirect("/login", http.StatusFound)
	}
	return c.SendFile("./static/account.html")
}

func (h *handlers) siteAccountData(c *fiber.Ctx) error {
	accountID, err := h.siteSessionAccountID(c)
	if err != nil || accountID == "" {
		return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "not signed in"})
	}
	key, _, err := ensureAccountAPIKey(accountID, "", "")
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load API key"})
	}
	var name, email, plan, handle, createdAt string
	_ = database.DB.QueryRow(
		`SELECT name, COALESCE(email, ''), plan, COALESCE(handle, ''), created_at
		   FROM accounts
		  WHERE id = ?1`,
		accountID,
	).Scan(&name, &email, &plan, &handle, &createdAt)

	var runsUsed int
	_ = database.DB.QueryRow(
		`SELECT COUNT(*)
		   FROM runs r
		   JOIN api_keys k ON k.id = r.api_key_id
		  WHERE COALESCE(k.account_id, k.id) = ?1
		    AND r.created_at >= datetime('now', 'start of month')`,
		accountID,
	).Scan(&runsUsed)

	var lastUsed string
	_ = database.DB.QueryRow(
		`SELECT COALESCE(MAX(created_at), '')
		   FROM usage_events
		  WHERE account_id = ?1`,
		accountID,
	).Scan(&lastUsed)
	runsLimit := 25
	if strings.EqualFold(strings.TrimSpace(plan), "pro") {
		runsLimit = 200
	}
	return c.JSON(fiber.Map{
		"account_id":         accountID,
		"name":               displayAccountName(name, email),
		"email":              email,
		"plan":               plan,
		"handle":             handle,
		"joined_at":          createdAt,
		"api_key":            key.Key,
		"api_key_created_at": key.CreatedAt.UTC().Format(time.RFC3339),
		"runs_used":          runsUsed,
		"runs_limit":         runsLimit,
		"last_used_at":       lastUsed,
	})
}

func displayAccountName(name, email string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if strings.TrimSpace(email) != "" {
		return email
	}
	return "BotTrade account"
}

func (h *handlers) siteLogout(c *fiber.Ctx) error {
	token := c.Cookies("bt_session")
	if token != "" {
		_, _ = database.DB.Exec(
			`UPDATE account_sessions SET revoked_at = CURRENT_TIMESTAMP WHERE token_hash = ?1`,
			hashToken(token),
		)
	}
	c.Cookie(&fiber.Cookie{Name: "bt_session", Value: "", Path: "/", MaxAge: -1})
	return c.Redirect("/", http.StatusFound)
}

func (h *handlers) createSiteSession(accountID string) (string, error) {
	token, err := randomOAuthToken("bt_site_")
	if err != nil {
		return "", err
	}
	_, err = database.DB.Exec(
		`INSERT INTO account_sessions (token_hash, account_id, expires_at)
		 VALUES (?1, ?2, ?3)`,
		hashToken(token), accountID, time.Now().UTC().Add(siteSessionTTL).Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (h *handlers) siteSessionAccountID(c *fiber.Ctx) (string, error) {
	token := c.Cookies("bt_session")
	if token == "" {
		return "", sql.ErrNoRows
	}
	var accountID, expiresAt, revokedAt string
	err := database.DB.QueryRow(
		`SELECT account_id, expires_at, COALESCE(revoked_at, '')
		   FROM account_sessions
		  WHERE token_hash = ?1`,
		hashToken(token),
	).Scan(&accountID, &expiresAt, &revokedAt)
	if err != nil {
		return "", err
	}
	exp, _ := time.Parse(time.RFC3339, expiresAt)
	if revokedAt != "" || time.Now().UTC().After(exp) {
		return "", sql.ErrNoRows
	}
	return accountID, nil
}

func (h *handlers) oauthCallback(c *fiber.Ctx) error {
	provider := c.Params("provider")
	return h.completeOAuthProviderCallback(c, provider, "/oauth/callback/"+provider)
}

func (h *handlers) completeOAuthProviderCallback(c *fiber.Ctx, provider, providerCallbackPath string) error {
	state := c.Query("state")
	if state == "" || state != c.Cookies("bt_oauth_state") {
		return c.Status(http.StatusBadRequest).SendString("invalid oauth state")
	}
	requestID := strings.SplitN(state, ".", 2)[0]
	authReq, err := loadOAuthAuthRequest(requestID)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString("authorization request expired")
	}
	profile, err := h.fetchOAuthProfile(provider, c.Query("code"), providerCallbackPath)
	if err != nil {
		return c.Status(http.StatusBadRequest).SendString(err.Error())
	}
	accountID, created, err := upsertOAuthAccount(profile)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account")
	}
	key, keyCreated, err := ensureAccountAPIKey(accountID, profile.Name, profile.Email)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account key")
	}
	h.captureAuth(c, accountID, created, profile, "mcp")
	if keyCreated {
		h.Analytics.Capture(accountID, "api_key_issued", analytics.Props().
			Set("plan", key.Plan).Set("flow", "mcp"))
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

func (h *handlers) providerAuthURL(provider, state, callbackPath string) (string, error) {
	callback := strings.TrimRight(h.AppBaseURL, "/") + callbackPath
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

func (h *handlers) fetchOAuthProfile(provider, code, callbackPath string) (oauthProfile, error) {
	if code == "" {
		return oauthProfile{}, fmt.Errorf("missing provider code")
	}
	switch provider {
	case "google":
		return h.fetchGoogleProfile(code, callbackPath)
	case "github":
		return h.fetchGitHubProfile(code, callbackPath)
	default:
		return oauthProfile{}, fmt.Errorf("unknown provider")
	}
}

func (h *handlers) fetchGoogleProfile(code, callbackPath string) (oauthProfile, error) {
	token, err := exchangeProviderCode("https://oauth2.googleapis.com/token", url.Values{
		"client_id":     {h.GoogleClientID},
		"client_secret": {h.GoogleClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {strings.TrimRight(h.AppBaseURL, "/") + callbackPath},
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

func (h *handlers) fetchGitHubProfile(code, callbackPath string) (oauthProfile, error) {
	token, err := exchangeProviderCode("https://github.com/login/oauth/access_token", url.Values{
		"client_id":     {h.GitHubClientID},
		"client_secret": {h.GitHubClientSecret},
		"code":          {code},
		"redirect_uri":  {strings.TrimRight(h.AppBaseURL, "/") + callbackPath},
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

func upsertOAuthAccount(profile oauthProfile) (string, bool, error) {
	var accountID string
	err := database.DB.QueryRow(
		`SELECT account_id FROM account_identities WHERE provider = ?1 AND provider_user_id = ?2`,
		profile.Provider, profile.ProviderUserID,
	).Scan(&accountID)
	if err == nil {
		return accountID, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}
	accountID = uuid.NewString()
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = profile.Provider + " user"
	}
	tx, err := database.DB.Begin()
	if err != nil {
		return "", false, err
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
		return "", false, err
	}
	_, err = tx.Exec(
		`INSERT INTO account_identities (account_id, provider, provider_user_id, email, name)
		 VALUES (?1, ?2, ?3, ?4, ?5)`,
		accountID, profile.Provider, profile.ProviderUserID, profile.Email, name,
	)
	if err != nil {
		return "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return accountID, true, nil
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
	return renderAuthPage(
		"Connect BotTrade",
		"Sign in once to let your agent run market-simulator scenarios with your BotTrade account.",
		google,
		github,
	)
}

func (h *handlers) renderSiteLogin() string {
	return renderAuthPage(
		"Sign in to BotTrade",
		"Access your API key, benchmark runs, quota, billing, and leaderboard identity.",
		"/auth/login/google",
		"/auth/login/github",
	)
}

func renderAuthPage(title, copy, googleHref, githubHref string) string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(title) + ` · BotTrade</title>
<meta name="description" content="` + html.EscapeString(copy) + `">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<style>
:root{--bg:#fff;--bg-2:#f5f5f5;--ink:#0a0a0a;--ink-2:#2a2a2a;--ink-3:#6a6a6a;--rule:#0a0a0a;--rule-2:#d8d8d8;--accent:#ff6a00;--accent-soft:#fff1e6}
*{box-sizing:border-box}html,body{margin:0;padding:0}body{background:var(--bg);color:var(--ink);font-family:"IBM Plex Sans",system-ui,sans-serif;font-size:15px;line-height:1.45;-webkit-font-smoothing:antialiased}
.topbar{border-bottom:1px solid var(--rule);padding:14px 32px;display:flex;align-items:center;gap:28px}
.brand{font-weight:700;font-size:18px;letter-spacing:-.5px;display:flex;align-items:center;gap:6px;text-decoration:none;color:var(--ink)}
.dot{width:8px;height:8px;background:var(--accent);border-radius:50%;display:inline-block}.slash{color:var(--ink-3);font-weight:400}.crumbs{font-size:13px;color:var(--ink-3)}.crumbs .here{color:var(--ink);font-weight:500}
.wrap{max-width:1120px;margin:0 auto;padding:0 32px 80px}
.auth{min-height:calc(100vh - 56px);display:grid;grid-template-columns:minmax(0,1fr) 420px;gap:48px;align-items:center;padding:54px 0}
.kicker{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:2px;color:var(--ink-3);margin-bottom:14px}.kicker span{background:var(--accent);color:#fff;padding:2px 8px;border-radius:3px;letter-spacing:1px}
h1{font-size:64px;font-weight:800;letter-spacing:-2px;line-height:.95;margin:0 0 16px;max-width:720px}h1 em{color:var(--accent);font-style:italic}
.lede{font-size:17px;color:var(--ink-2);max-width:620px;line-height:1.55;margin:0}.panel{border:1px solid var(--rule);border-radius:8px;background:var(--bg);padding:24px}
.panel h2{font-size:22px;line-height:1.1;margin:0 0 8px;letter-spacing:-.5px}.panel p{color:var(--ink-3);margin:0 0 20px;line-height:1.5}
.auth-btn{display:flex;align-items:center;justify-content:center;gap:10px;width:100%;min-height:46px;border-radius:6px;border:1px solid var(--rule);text-decoration:none;font-weight:700;color:var(--ink);background:#fff;margin-bottom:10px}
.auth-btn:hover{border-color:var(--accent);color:var(--accent)}.auth-btn.github{background:var(--ink);color:#fff}.auth-btn.github:hover{background:var(--accent);border-color:var(--accent);color:#fff}
.fine{font-family:"IBM Plex Mono",ui-monospace,monospace;font-size:12px;color:var(--ink-3);padding-top:12px;border-top:1px solid var(--rule-2);margin-top:16px}
@media (max-width:860px){.topbar{padding:14px 20px}.wrap{padding:0 20px 48px}.auth{grid-template-columns:1fr;gap:28px;align-items:start;padding:36px 0}h1{font-size:44px}.panel{padding:20px}}
</style>
</head>
<body>
  <header class="topbar">
    <a href="/" class="brand"><span class="dot"></span>bot<span class="slash">/</span>trade</a>
    <div class="crumbs"><span class="here">Account</span></div>
  </header>
  <main class="wrap">
    <section class="auth">
      <div>
        <div class="kicker"><span>BotTrade</span> account access</div>
        <h1>` + html.EscapeString(title) + ` for <em>agent runs.</em></h1>
        <p class="lede">` + html.EscapeString(copy) + ` Usage, runs, billing, and leaderboard identity stay attached to your BotTrade account.</p>
      </div>
      <div class="panel">
        <h2>Continue</h2>
        <p>Choose a provider to access your BotTrade account.</p>
        <a class="auth-btn" href="` + html.EscapeString(googleHref) + `">Continue with Google</a>
        <a class="auth-btn github" href="` + html.EscapeString(githubHref) + `">Continue with GitHub</a>
        <div class="fine">MCP clients connect through mcp.bot-trade.org. This page is for human sign-in.</div>
      </div>
    </section>
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

// captureAuth records signup/sign-in analytics with the visitor's real IP so
// PostHog GeoIP works behind Cloudflare, and links person properties to the
// account distinct_id used everywhere else.
func (h *handlers) captureAuth(c *fiber.Ctx, accountID string, created bool, profile oauthProfile, flow string) {
	event := "account_signed_in"
	if created {
		event = "account_signed_up"
	}
	h.Analytics.Identify(accountID, analytics.Props().
		Set("email", profile.Email).
		Set("name", profile.Name).
		Set("auth_provider", profile.Provider))
	h.Analytics.Capture(accountID, event, analytics.Props().
		Set("provider", profile.Provider).
		Set("flow", flow).
		Set("$ip", clientIP(c)))
}

// clientIP resolves the real visitor IP behind the Cloudflare + Railway edge.
func clientIP(c *fiber.Ctx) string {
	if ip := strings.TrimSpace(c.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return c.IP()
}
