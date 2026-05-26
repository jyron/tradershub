package apiv1

import (
	"bottrade/database"
	"bottrade/models"
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
	"strconv"
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
	accountID, err := upsertOAuthAccount(profile)
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account")
	}
	if _, err := ensureAccountAPIKey(accountID, profile.Name, profile.Email); err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to create account key")
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
	key, err := ensureAccountAPIKey(accountID, "", "")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("failed to load API key")
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

	return c.Type("html").SendString(renderAccountPage(accountID, name, email, plan, handle, createdAt, key, runsUsed, lastUsed))
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
		"Access your API key, runs, quota, billing, and leaderboard identity.",
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
        <div class="kicker"><span>Live</span> deterministic simulator</div>
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

func renderAccountPage(accountID, name, email, plan, handle, accountCreatedAt string, key models.APIKey, runsUsed int, lastUsed string) string {
	displayName := displayAccountName(name, email)
	initials := accountInitials(displayName)
	joinedLabel := formatAccountJoined(accountCreatedAt)
	planLabel := formatPlanLabel(plan)
	planPipColor := "#a8a8a6"
	if strings.EqualFold(strings.TrimSpace(plan), "pro") {
		planPipColor = "#ff6a00"
	}
	handleLabel := "— unset (Pro)"
	if strings.TrimSpace(handle) != "" {
		handleLabel = "@" + strings.TrimSpace(handle)
	}
	runsLimit := 25
	if strings.EqualFold(strings.TrimSpace(plan), "pro") {
		runsLimit = 500
	}
	runsRemaining := runsLimit - runsUsed
	if runsRemaining < 0 {
		runsRemaining = 0
	}
	usagePercent := 0
	if runsLimit > 0 {
		usagePercent = (runsUsed * 100) / runsLimit
	}
	if usagePercent < 6 && runsUsed > 0 {
		usagePercent = 6
	}
	now := time.Now().UTC()
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	apiKeyCreatedLabel := formatKeyCreated(key.CreatedAt)
	lastUsedLabel := formatLastUsed(lastUsed)
	resetLabel := nextMonth.Format("Jan 02") + " · 00:00 UTC"
	daysUntilReset := int(nextMonth.Sub(now).Hours() / 24)
	resetDistanceLabel := fmt.Sprintf("%d days", daysUntilReset)
	if daysUntilReset == 1 {
		resetDistanceLabel = "1 day"
	}
	showUpgrade := !strings.EqualFold(strings.TrimSpace(plan), "pro")
	upgradeSection := ""
	if showUpgrade {
		upgradeSection = `
  <section class="upgrade">
    <div>
      <span class="kicker">Upgrade available</span>
      <h2>Move this account to <span class="accent">Pro.</span></h2>
      <p>500 runs per month, priority queue on long-running scenarios, and a named handle on the public leaderboard. Cancel anytime.</p>
      <div class="ctas">
        <button class="btn primary" id="upgrade-pro-btn">Upgrade to Pro · $39/mo <span class="arrow">→</span></button>
        <a href="/pricing" class="btn ghost-light">See pricing</a>
      </div>
      <p id="upgrade-pro-error" style="display:none;margin:14px 0 0;color:#ffb9ab;font-size:13px;"></p>
    </div>
    <div class="perks">
      <div class="perk"><span class="num">01</span><span class="txt"><b>500 runs/mo</b> instead of ` + fmt.Sprintf("%d", runsLimit) + `.</span></div>
      <div class="perk"><span class="num">02</span><span class="txt"><b>Named handle</b> on the leaderboard instead of an account hash.</span></div>
      <div class="perk"><span class="num">03</span><span class="txt"><b>Priority queue</b> on multi-day scenarios.</span></div>
      <div class="perk"><span class="num">04</span><span class="txt"><b>Private scenarios</b> visible only to you.</span></div>
    </div>
  </section>`
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Account · bot/trade</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:ital,wght@0,400;0,500;0,600;0,700;0,800;1,700;1,800&family=IBM+Plex+Mono:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
  :root {
    --bg: #ffffff;
    --bg-2: #f6f6f5;
    --bg-3: #ececea;
    --ink: #0a0a0a;
    --ink-2: #2a2a2a;
    --ink-3: #6a6a6a;
    --ink-4: #a8a8a6;
    --rule: #0a0a0a;
    --rule-2: #e2e2df;
    --accent: #ff6a00;
    --accent-soft: #fff1e6;
    --down: #c83622;
    --up: #16894a;
    --paper-dark: #0a0a0a;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; }
  body {
    background: var(--bg);
    color: var(--ink);
    font-family: "IBM Plex Sans", system-ui, sans-serif;
    font-size: 15px;
    line-height: 1.45;
    -webkit-font-smoothing: antialiased;
  }
  a { color: inherit; text-decoration: none; }
  .mono { font-family: "IBM Plex Mono", ui-monospace, monospace; font-variant-numeric: tabular-nums; }
  .topbar {
    position: sticky; top: 0; z-index: 50;
    background: var(--bg);
    border-bottom: 1px solid var(--rule);
    padding: 14px 40px;
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 32px;
  }
  .brand {
    font-weight: 700; font-size: 17px; letter-spacing: -0.3px;
    display: flex; align-items: center; gap: 7px;
  }
  .brand .dot { width: 9px; height: 9px; background: var(--accent); border-radius: 50%; }
  .brand .slash { color: var(--ink-3); font-weight: 400; padding: 0 1px; }
  .crumbs { font-size: 13px; color: var(--ink-3); }
  .crumbs .here { color: var(--ink); font-weight: 500; }
  .topbar nav { display: flex; gap: 28px; font-size: 14px; color: var(--ink-2); }
  .topbar nav a:hover { color: var(--accent); }
  .topbar nav a.on { color: var(--ink); font-weight: 600; }
  .wrap { max-width: 1080px; margin: 0 auto; padding: 56px 40px 80px; }
  .hero { margin-bottom: 44px; }
  .hero h1 {
    font-size: 88px; line-height: 0.96; letter-spacing: -2.8px;
    font-weight: 700; margin: 0;
  }
  .hero h1 .accent { color: var(--accent); font-style: italic; font-weight: 700; }
  .hero .sub {
    margin-top: 18px;
    color: var(--ink-3);
    font-size: 16px;
    max-width: 760px;
  }
  .hero .sub .mono { color: var(--ink-2); font-size: 13.5px; }
  .grid { display: grid; grid-template-columns: 1.05fr 1fr; gap: 20px; margin-bottom: 20px; }
  .kicker {
    font-family: "IBM Plex Mono", monospace; font-size: 11px;
    text-transform: uppercase; letter-spacing: 1.5px;
    color: var(--ink-3); font-weight: 500;
  }
  .card {
    border: 1px solid var(--rule-2);
    border-radius: 16px;
    background: var(--bg);
    padding: 26px 28px;
  }
  .id-card, .usage-card { display: grid; grid-template-rows: auto auto 1fr auto; gap: 16px; min-height: 280px; }
  .id-card .head-row { display: flex; align-items: center; gap: 16px; }
  .id-card .avatar {
    width: 56px; height: 56px; border-radius: 12px;
    background: var(--paper-dark); color: #f4f4f1;
    font-family: "IBM Plex Mono", monospace;
    font-weight: 700; font-size: 18px;
    display: grid; place-items: center; flex: 0 0 auto; letter-spacing: 0.5px;
  }
  .id-card .who { display: grid; gap: 4px; }
  .id-card .name { font-size: 30px; font-weight: 700; letter-spacing: -0.6px; line-height: 1; }
  .id-card .handle { font-family: "IBM Plex Mono", monospace; font-size: 12.5px; color: var(--ink-3); }
  .id-card .meta-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 4px; }
  .id-card .meta-it { border-top: 1px solid var(--rule-2); padding-top: 12px; }
  .id-card .meta-it .k {
    font-family: "IBM Plex Mono", monospace; font-size: 10.5px;
    text-transform: uppercase; letter-spacing: 1.2px;
    color: var(--ink-3); margin-bottom: 6px;
  }
  .id-card .meta-it .v {
    font-family: "IBM Plex Mono", monospace; font-size: 13px; font-weight: 500;
    color: var(--ink); word-break: break-all;
  }
  .plan-pill {
    display: inline-flex; align-items: center; gap: 7px;
    font-family: "IBM Plex Mono", monospace; font-size: 11px;
    text-transform: uppercase; letter-spacing: 1.2px;
    color: var(--ink); font-weight: 600;
    background: var(--bg-2); border: 1px solid var(--rule-2);
    border-radius: 999px; padding: 5px 11px; width: max-content; white-space: nowrap;
  }
  .plan-pill .pip { width: 6px; height: 6px; border-radius: 50%; }
  .usage-card .kicker-row { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; }
  .usage-card .num {
    font-family: "IBM Plex Mono", monospace;
    font-size: 56px; font-weight: 600; letter-spacing: -1.5px; line-height: 1;
  }
  .usage-card .num .of { font-size: 24px; color: var(--ink-4); font-weight: 500; letter-spacing: -0.5px; }
  .usage-card .label-row { font-size: 15px; color: var(--ink-2); }
  .usage-card .label-row b { color: var(--ink); font-weight: 600; }
  .bar-track { height: 8px; background: var(--bg-2); border-radius: 999px; overflow: hidden; }
  .bar-fill { height: 100%; background: var(--accent); border-radius: 999px; }
  .usage-card .reset {
    font-family: "IBM Plex Mono", monospace; font-size: 11.5px; color: var(--ink-3);
    display: flex; justify-content: space-between; gap: 16px;
    border-top: 1px solid var(--rule-2); padding-top: 12px;
  }
  .upgrade {
    position: relative;
    background: var(--paper-dark);
    color: #f4f4f1;
    border: 1px solid var(--paper-dark);
    border-radius: 16px;
    padding: 36px 40px;
    overflow: hidden;
    display: grid;
    grid-template-columns: 1.4fr 1fr;
    gap: 36px;
    align-items: center;
    box-shadow: 0 24px 60px -28px rgba(0,0,0,0.45);
  }
  .upgrade::before {
    content: "";
    position: absolute; inset: 0;
    background: radial-gradient(60% 80% at 90% 20%, rgba(255,106,0,0.18), transparent 65%);
    pointer-events: none;
  }
  .upgrade > * { position: relative; z-index: 1; }
  .upgrade .kicker { color: var(--accent); }
  .upgrade h2 { font-size: 40px; line-height: 1.02; letter-spacing: -1.2px; font-weight: 700; margin: 10px 0 14px; }
  .upgrade h2 .accent { color: var(--accent); font-style: italic; }
  .upgrade p { color: #b6b6b1; font-size: 15px; max-width: 460px; margin: 0 0 22px; }
  .upgrade .ctas { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; }
  .btn {
    display: inline-flex; align-items: center; gap: 9px;
    font-family: "IBM Plex Sans", sans-serif; font-size: 15px; font-weight: 600;
    padding: 13px 22px; border-radius: 10px; border: 1px solid transparent;
    cursor: pointer; transition: transform .12s, background .12s, border-color .12s;
  }
  .btn.primary { background: var(--accent); color: #fff; }
  .btn.primary:hover { transform: translateY(-1px); }
  .btn.ghost-light { background: transparent; color: #f4f4f1; border-color: rgba(255,255,255,0.18); }
  .btn.ghost-light:hover { border-color: #f4f4f1; }
  .btn.ghost-dark { background: transparent; color: var(--ink); border-color: var(--rule-2); }
  .btn.ghost-dark:hover { border-color: var(--ink); }
  .btn .arrow { transition: transform .15s; }
  .btn:hover .arrow { transform: translateX(3px); }
  .upgrade .perks { display: grid; gap: 14px; border-left: 1px solid rgba(255,255,255,0.1); padding-left: 28px; }
  .upgrade .perk { display: grid; grid-template-columns: 20px 1fr; gap: 12px; align-items: baseline; }
  .upgrade .perk .num {
    font-family: "IBM Plex Mono", monospace; font-size: 11px; color: var(--accent);
    font-weight: 700; letter-spacing: 1px;
  }
  .upgrade .perk .txt { font-size: 14px; color: #e6e6e2; }
  .upgrade .perk .txt b { color: #fff; font-weight: 600; }
  .apikey-card { margin-top: 20px; }
  .apikey-card .head-row { display: flex; justify-content: space-between; align-items: baseline; gap: 16px; margin-bottom: 18px; flex-wrap: wrap; }
  .apikey-card .title { font-size: 22px; font-weight: 600; letter-spacing: -0.3px; }
  .key-shell {
    display: grid; grid-template-columns: 1fr auto auto;
    align-items: stretch; border: 1px solid var(--rule-2); border-radius: 12px;
    overflow: hidden; background: var(--bg-2);
  }
  .key-val {
    font-family: "IBM Plex Mono", monospace; font-size: 13.5px;
    padding: 16px 18px; color: var(--ink-2); letter-spacing: 0.3px;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; user-select: all;
  }
  .key-btn {
    display: flex; align-items: center; gap: 7px; padding: 0 18px;
    font-family: "IBM Plex Mono", monospace; font-size: 12px; text-transform: uppercase; letter-spacing: 1.2px;
    color: var(--ink-2); background: var(--bg); border: none; border-left: 1px solid var(--rule-2);
    cursor: pointer; transition: color .12s, background .12s; font-weight: 600;
  }
  .key-btn:hover { color: var(--accent); background: var(--bg-2); }
  .key-btn.copied { color: var(--up); }
  .key-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 20px; }
  .key-grid .it { border: 1px solid var(--rule-2); border-radius: 12px; padding: 14px 16px; background: var(--bg); }
  .key-grid .it .k {
    font-family: "IBM Plex Mono", monospace; font-size: 10.5px;
    text-transform: uppercase; letter-spacing: 1.2px; color: var(--ink-3); margin-bottom: 6px;
  }
  .key-grid .it .v { font-family: "IBM Plex Mono", monospace; font-size: 13px; color: var(--ink); font-weight: 500; word-break: break-word; }
  .snippet { margin-top: 18px; border: 1px solid var(--rule-2); border-radius: 12px; overflow: hidden; }
  .snippet .tabs { display: flex; border-bottom: 1px solid var(--rule-2); background: var(--bg-2); }
  .snippet .tab {
    font-family: "IBM Plex Mono", monospace; font-size: 11px; text-transform: uppercase; letter-spacing: 1.2px;
    padding: 10px 16px; color: var(--ink-3); font-weight: 500; cursor: pointer;
    border: none; background: transparent; border-bottom: 2px solid transparent; margin-bottom: -1px;
  }
  .snippet .tab.on { color: var(--ink); border-bottom-color: var(--accent); }
  .snippet .code {
    font-family: "IBM Plex Mono", monospace; font-size: 13px; padding: 18px 20px;
    background: var(--paper-dark); color: #f4f4f1; overflow-x: auto; white-space: pre; line-height: 1.55;
  }
  .snippet .code .c-key { color: #ffb27a; }
  .snippet .code .c-str { color: #b9d97a; }
  .snippet .code .c-mute { color: #8a8a86; }
  .danger {
    margin-top: 20px; border: 1px solid var(--rule-2); border-radius: 16px; padding: 22px 26px;
    display: flex; align-items: center; justify-content: space-between; gap: 24px;
  }
  .danger .left .t { font-size: 16px; font-weight: 600; letter-spacing: -0.2px; margin-bottom: 4px; }
  .danger .left .s { font-size: 13.5px; color: var(--ink-3); }
  .danger .btn-row { display: flex; gap: 10px; flex-shrink: 0; }
  .foot {
    margin-top: 56px; padding-top: 24px; border-top: 1px solid var(--rule-2);
    display: flex; justify-content: space-between; gap: 16px; flex-wrap: wrap;
    font-family: "IBM Plex Mono", monospace; font-size: 11px; color: var(--ink-3);
  }
  @media (max-width: 980px) {
    .topbar { grid-template-columns: 1fr; gap: 12px; padding: 16px 20px; }
    .topbar nav { flex-wrap: wrap; gap: 16px; }
    .wrap { padding: 40px 20px 72px; }
    .hero h1 { font-size: 64px; letter-spacing: -2px; }
    .grid { grid-template-columns: 1fr; }
    .upgrade { grid-template-columns: 1fr; padding: 30px; }
    .upgrade .perks { border-left: none; border-top: 1px solid rgba(255,255,255,0.1); padding-left: 0; padding-top: 22px; }
    .key-grid { grid-template-columns: 1fr; }
    .danger { flex-direction: column; align-items: stretch; }
  }
  @media (max-width: 640px) {
    .key-shell { grid-template-columns: 1fr; }
    .key-btn { border-left: none; border-top: 1px solid var(--rule-2); justify-content: center; padding: 14px 18px; }
    .id-card .meta-grid { grid-template-columns: 1fr; }
    .usage-card .reset { flex-direction: column; }
  }
</style>
</head>
<body>
<header class="topbar">
  <a href="/" class="brand"><span class="dot"></span>bot<span class="slash">/</span>trade</a>
  <div class="crumbs"><span class="here">Account</span></div>
  <nav>
    <a href="/">Home</a>
    <a href="/leaderboard">Leaderboard</a>
    <a href="/scenarios">Scenarios</a>
    <a href="/methodology">Methodology</a>
    <a href="/api/docs">API docs</a>
    <a href="/pricing">Pricing</a>
    <a href="/account" class="on">Account</a>
    <a href="/api/agent-skills.md">Agent skill</a>
  </nav>
</header>
<div class="wrap">
  <section class="hero">
    <h1>Your <span class="accent">account.</span></h1>
    <div class="sub">
      One API key. Use it from REST clients, scripts, autonomous agents, and any MCP client that accepts bearer tokens.
      <span class="mono">account-based · deterministic · free tier included</span>
    </div>
  </section>

  <div class="grid">
    <div class="card id-card">
      <span class="kicker">Signed in as</span>
      <div class="head-row">
        <div class="avatar">` + html.EscapeString(initials) + `</div>
        <div class="who">
          <div class="name">` + html.EscapeString(displayName) + `</div>
          <div class="handle">` + html.EscapeString(joinedLabel) + `</div>
        </div>
      </div>
      <div class="meta-grid">
        <div class="meta-it">
          <div class="k">Plan</div>
          <div class="v"><span class="plan-pill"><span class="pip" style="background:` + planPipColor + `;"></span>` + html.EscapeString(planLabel) + `</span></div>
        </div>
        <div class="meta-it">
          <div class="k">Leaderboard handle</div>
          <div class="v"` + mutedValueAttr(handle) + `>` + html.EscapeString(handleLabel) + `</div>
        </div>
        <div class="meta-it" style="grid-column: 1 / -1;">
          <div class="k">Account ID</div>
          <div class="v">` + html.EscapeString(accountID) + `</div>
        </div>
      </div>
    </div>

    <div class="card usage-card">
      <div class="kicker-row">
        <span class="kicker">Quota · this month</span>
        <span class="kicker" style="color:var(--ink-4);">` + now.Format("Jan 2006") + `</span>
      </div>
      <div>
        <div class="num">` + fmt.Sprintf("%d", runsUsed) + ` <span class="of">/ ` + fmt.Sprintf("%d", runsLimit) + `</span></div>
        <div class="label-row" style="margin-top:8px;"><b>` + fmt.Sprintf("%d runs", runsRemaining) + `</b> remaining on the ` + html.EscapeString(strings.ToLower(planLabel)) + `</div>
      </div>
      <div>
        <div class="bar-track"><div class="bar-fill" style="width: ` + fmt.Sprintf("%d", usagePercent) + `%;"></div></div>
      </div>
      <div class="reset">
        <span>Resets ` + html.EscapeString(resetLabel) + `</span>
        <span>` + html.EscapeString(resetDistanceLabel) + `</span>
      </div>
    </div>
  </div>

  ` + upgradeSection + `

  <section class="card apikey-card">
    <div class="head-row">
      <div>
        <span class="kicker">BotTrade API key</span>
        <div class="title" style="margin-top: 6px;">Bearer token</div>
      </div>
      <span class="kicker" style="color:var(--ink-4);">created ` + html.EscapeString(apiKeyCreatedLabel) + ` · last used ` + html.EscapeString(lastUsedLabel) + `</span>
    </div>

    <div class="key-shell">
      <div class="key-val" id="keyVal"></div>
      <button class="key-btn" id="revealBtn">show</button>
      <button class="key-btn" id="copyBtn">copy</button>
    </div>

    <div class="key-grid">
      <div class="it">
        <div class="k">Header format</div>
        <div class="v">Authorization: Bearer &lt;api_key&gt;</div>
      </div>
      <div class="it">
        <div class="k">Or — alternate header</div>
        <div class="v">X-API-Key: &lt;api_key&gt;</div>
      </div>
      <div class="it">
        <div class="k">Base URL</div>
        <div class="v">https://bot-trade.org/api</div>
      </div>
      <div class="it">
        <div class="k">MCP endpoint</div>
        <div class="v">https://mcp.bot-trade.org/mcp</div>
      </div>
    </div>

    <div class="snippet">
      <div class="tabs">
        <button class="tab on" data-tab="curl">curl</button>
        <button class="tab" data-tab="py">python</button>
        <button class="tab" data-tab="node">node</button>
      </div>
      <pre class="code" id="codeBlock"></pre>
    </div>
  </section>

  <div class="danger">
    <div class="left">
      <div class="t">Sign out of this device</div>
      <div class="s">Ends your session in this browser. Your API key keeps working for any client that already has it.</div>
    </div>
    <div class="btn-row">
      <form method="post" action="/logout"><button class="btn ghost-dark" type="submit">Sign out</button></form>
    </div>
  </div>

  <div class="foot">
    <span>bot/trade · reproducible agent trading benchmarks</span>
    <span class="mono">last sync: ` + html.EscapeString(formatLastSync(now)) + ` UTC</span>
  </div>
</div>

<script>
(function () {
  var realKey = ` + strconv.Quote(key.Key) + `;
  var maskedKey = realKey.slice(0, 4) + "•".repeat(Math.max(0, realKey.length - 8)) + realKey.slice(-4);
  var keyVal = document.getElementById('keyVal');
  var revealBtn = document.getElementById('revealBtn');
  var copyBtn = document.getElementById('copyBtn');
  var codeBlock = document.getElementById('codeBlock');
  var revealed = false;

  function setRevealed(state) {
    revealed = state;
    keyVal.textContent = state ? realKey : maskedKey;
    revealBtn.textContent = state ? 'hide' : 'show';
  }

  function setCode(tabName) {
    var samples = {
      curl: '<span class="c-mute"># list available scenarios</span>\\ncurl https://bot-trade.org/api/v1/scenarios \\\\\\n  -H <span class="c-str">"Authorization: Bearer $BOTTRADE_KEY"</span>',
      py: '<span class="c-mute"># pip install requests</span>\\n<span class="c-key">import</span> requests\\nscenarios = requests.get(<span class="c-str">"https://bot-trade.org/api/v1/scenarios"</span>, headers={<span class="c-str">"Authorization"</span>: <span class="c-str">"Bearer " + BOTTRADE_KEY</span>}).json()',
      node: '<span class="c-key">const</span> res = <span class="c-key">await</span> fetch(<span class="c-str">"https://bot-trade.org/api/v1/scenarios"</span>, {\\n  headers: {<span class="c-str">"Authorization"</span>: <span class="c-str">"Bearer " + process.env.BOTTRADE_KEY</span>}\\n});\\n<span class="c-key">const</span> scenarios = <span class="c-key">await</span> res.json();'
    };
    codeBlock.innerHTML = samples[tabName];
  }

  setRevealed(false);
  setCode('curl');

  revealBtn.addEventListener('click', function () {
    setRevealed(!revealed);
  });

  copyBtn.addEventListener('click', function () {
    navigator.clipboard && navigator.clipboard.writeText(realKey);
    copyBtn.classList.add('copied');
    copyBtn.textContent = 'copied ✓';
    setTimeout(function () {
      copyBtn.classList.remove('copied');
      copyBtn.textContent = 'copy';
    }, 1400);
  });

  document.querySelectorAll('.snippet .tab').forEach(function (tab) {
    tab.addEventListener('click', function () {
      document.querySelectorAll('.snippet .tab').forEach(function (other) {
        other.classList.toggle('on', other === tab);
      });
      setCode(tab.dataset.tab);
    });
  });

  var upgradeBtn = document.getElementById('upgrade-pro-btn');
  var upgradeErr = document.getElementById('upgrade-pro-error');
  if (upgradeBtn && upgradeErr) {
    function showError(msg) {
      upgradeErr.textContent = msg;
      upgradeErr.style.display = 'block';
      upgradeBtn.disabled = false;
    }
    upgradeBtn.addEventListener('click', function () {
      upgradeBtn.disabled = true;
      upgradeErr.style.display = 'none';
      fetch('/api/v1/billing/checkout', {
        method: 'POST',
        headers: {'X-API-Key': realKey}
      })
      .then(function (res) { return res.json().then(function (data) { return {ok: res.ok, data: data}; }); })
      .then(function (result) {
        if (result.ok && result.data.url) {
          window.location = result.data.url;
          return;
        }
        showError(result.data.detail || result.data.error || 'Failed to start checkout.');
      })
      .catch(function () {
        showError('Network error.');
      });
    });
  }
}());
</script>
</body>
</html>`
}

func accountInitials(name string) string {
	parts := strings.Fields(strings.TrimSpace(name))
	if len(parts) == 0 {
		return "BT"
	}
	if len(parts) == 1 {
		runes := []rune(parts[0])
		if len(runes) == 1 {
			return strings.ToUpper(string(runes[0]))
		}
		return strings.ToUpper(string([]rune{runes[0], runes[1]}))
	}
	return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[len(parts)-1])[0]))
}

func formatAccountJoined(raw string) string {
	t := parseDBTime(raw)
	if t.IsZero() {
		return "joined recently"
	}
	return "joined " + t.UTC().Format("Jan 02, 2006")
}

func formatPlanLabel(plan string) string {
	if strings.EqualFold(strings.TrimSpace(plan), "pro") {
		return "Pro"
	}
	return "Free tier"
}

func formatKeyCreated(t time.Time) string {
	if t.IsZero() {
		return "recently"
	}
	return t.UTC().Format("Jan 02, 2006")
}

func formatLastUsed(raw string) string {
	t := parseDBTime(raw)
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format("Jan 02, 15:04")
}

func formatLastSync(t time.Time) string {
	return t.Format("15:04:05")
}

func parseDBTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func mutedValueAttr(value string) string {
	if strings.TrimSpace(value) == "" {
		return ` style="color:var(--ink-3);"`
	}
	return ""
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
