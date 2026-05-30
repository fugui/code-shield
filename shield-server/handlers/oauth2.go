package handlers

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// oauth2States is the singleton state store for OAuth2 flow CSRF protection.
var oauth2States *StateStore

func init() {
	oauth2States = NewStateStore()
}

// GetAuthConfig returns the public-facing authentication configuration.
// This endpoint is unauthenticated so the login page can determine which login methods to show.
// It intentionally does NOT expose sensitive values like client_secret or jwt_secret.
func GetAuthConfig(c *gin.Context) {
	authCfg := models.AppConfig.Auth
	c.JSON(http.StatusOK, gin.H{
		"oauth2_enabled":         authCfg.OAuth2.Enabled,
		"password_login_enabled": authCfg.PasswordLoginEnabled,
	})
}

// StartOAuth2Flow initiates the OAuth2 Authorization Code flow.
// It generates a PKCE challenge, stores the state, and redirects the user to the IdP.
func StartOAuth2Flow(c *gin.Context) {
	oauth2Cfg := models.AppConfig.Auth.OAuth2
	if !oauth2Cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth2 SSO is not enabled"})
		return
	}

	state, _, codeChallenge, err := oauth2States.GenerateState()
	if err != nil {
		log.Printf("[OAuth2] Failed to generate state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate SSO login"})
		return
	}

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {oauth2Cfg.ClientID},
		"redirect_uri":          {oauth2Cfg.RedirectURL},
		"scope":                 {strings.Join(oauth2Cfg.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}

	authURL := oauth2Cfg.AuthURL + "?" + params.Encode()
	c.Redirect(http.StatusFound, authURL)
}

// OAuth2Callback handles the callback from the OAuth2 Identity Provider.
// It exchanges the authorization code for tokens, fetches user info, provisions the user,
// issues a local JWT, and redirects to the frontend with the token.
func OAuth2Callback(c *gin.Context) {
	oauth2Cfg := models.AppConfig.Auth.OAuth2
	if !oauth2Cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth2 SSO is not enabled"})
		return
	}

	// Check for IdP errors
	if errMsg := c.Query("error"); errMsg != "" {
		errDesc := c.Query("error_description")
		log.Printf("[OAuth2] IdP returned error: %s - %s", errMsg, errDesc)
		redirectToLoginWithError(c, "SSO 登录失败: "+errDesc)
		return
	}

	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		redirectToLoginWithError(c, "SSO 回调参数缺失")
		return
	}

	// Validate state (CSRF protection) and retrieve PKCE code_verifier
	codeVerifier, ok := oauth2States.ValidateAndConsume(state)
	if !ok {
		redirectToLoginWithError(c, "SSO 登录超时或状态无效，请重试")
		return
	}

	// Exchange authorization code for access token
	tokenData, err := exchangeCodeForToken(oauth2Cfg, code, codeVerifier)
	if err != nil {
		log.Printf("[OAuth2] Token exchange failed: %v", err)
		redirectToLoginWithError(c, "SSO Token 交换失败")
		return
	}

	accessToken, _ := tokenData["access_token"].(string)
	if accessToken == "" {
		log.Printf("[OAuth2] No access_token in response: %v", tokenData)
		redirectToLoginWithError(c, "SSO 未返回有效的 access_token")
		return
	}

	// Fetch user info from IdP
	userInfo, err := fetchUserInfo(oauth2Cfg.UserInfoURL, accessToken)
	if err != nil {
		log.Printf("[OAuth2] UserInfo fetch failed: %v", err)
		redirectToLoginWithError(c, "SSO 用户信息获取失败")
		return
	}

	// DEBUG: Print userinfo to console as JSON
	if userInfoBytes, err := json.Marshal(userInfo); err == nil {
		log.Printf("[OAuth2] DEBUG - Received UserInfo: %s", string(userInfoBytes))
	} else {
		log.Printf("[OAuth2] DEBUG - Received UserInfo (Go representation): %+v", userInfo)
	}

	// Extract user attributes using field mapping
	mapping := oauth2Cfg.FieldMapping
	username := getStringField(userInfo, mapping.Username)
	email := getStringField(userInfo, mapping.Email)
	name := getStringField(userInfo, mapping.Name)

	// Username is required for local user matching
	if username == "" {
		// Fallback to email as username if preferred_username is not available
		username = email
	}
	if username == "" {
		log.Printf("[OAuth2] No username found in userinfo: %v", userInfo)
		redirectToLoginWithError(c, "SSO 未返回用户标识信息")
		return
	}

	// Auto-provision or match local user
	var user models.User
	err = models.DB.Where("username = ?", username).First(&user).Error
	if err != nil {
		// Create new user via SSO auto-provisioning
		displayName := name
		if displayName == "" {
			displayName = username
		}

		user = models.User{
			Username: username,
			Name:     displayName,
			IsAdmin:  false, // Admin privileges are managed manually by administrators
			IsActive: true,
			Password: "$2a$10$SSO_USER_NO_PASSWORD_LOGIN", // Marker: SSO user, password login disabled
		}
		if err := models.DB.Create(&user).Error; err != nil {
			log.Printf("[OAuth2] Failed to auto-provision user %s: %v", username, err)
			redirectToLoginWithError(c, "SSO 用户自动开通失败")
			return
		}
		log.Printf("[OAuth2] Auto-provisioned new user: %s (%s)", username, displayName)
	} else {
		// Update name from IdP if it changed
		if name != "" && name != user.Name {
			models.DB.Model(&user).Update("name", name)
			user.Name = name
		}
	}

	if !user.IsActive {
		redirectToLoginWithError(c, "该账号已被管理员禁用")
		return
	}

	// Update last_login
	now := time.Now()
	models.DB.Model(&user).Update("last_login", now)

	// Issue Code Shield JWT
	jwtSecret := []byte(models.AppConfig.Auth.JWTSecret)
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Printf("[OAuth2] Failed to generate JWT: %v", err)
		redirectToLoginWithError(c, "登录凭证生成失败")
		return
	}

	// Redirect to frontend callback page with token in URL fragment (not exposed to server logs)
	frontendCallbackURL := buildFrontendCallbackURL(tokenString, email)
	c.Redirect(http.StatusFound, frontendCallbackURL)
}

// exchangeCodeForToken performs the OAuth2 token exchange.
func exchangeCodeForToken(cfg models.OAuth2Config, code, codeVerifier string) (map[string]interface{}, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code_verifier": {codeVerifier},
	}

	resp, err := http.PostForm(cfg.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return result, nil
}

// fetchUserInfo calls the OAuth2 UserInfo endpoint with the access token.
func fetchUserInfo(userInfoURL, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read userinfo response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	return result, nil
}

// getStringField safely extracts a string value from a map by key.
func getStringField(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// buildFrontendCallbackURL constructs the frontend OAuth callback URL with the JWT token.
// The token is passed via URL query parameter to the dedicated callback page,
// which then stores it in localStorage and redirects to the main app.
func buildFrontendCallbackURL(token, email string) string {
	// Build the frontend callback page URL
	externalURL := strings.TrimRight(models.AppConfig.Server.ExternalURL, "/")
	callbackPath := "/oauth2/callback"

	params := url.Values{
		"token": {token},
	}
	if email != "" {
		params.Set("email", email)
	}

	return externalURL + callbackPath + "?" + params.Encode()
}

// redirectToLoginWithError redirects the user back to the login page with an error message.
func redirectToLoginWithError(c *gin.Context, errorMsg string) {
	externalURL := strings.TrimRight(models.AppConfig.Server.ExternalURL, "/")
	loginURL := externalURL + "/login?sso_error=" + url.QueryEscape(errorMsg)
	c.Redirect(http.StatusFound, loginURL)
}
