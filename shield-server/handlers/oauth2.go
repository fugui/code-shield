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
	email := getStringField(userInfo, mapping.Email)

	// Parse the LDAP-style cn=... en=... string from the username mapping
	// to get the display Name (priority cn, fallback en)
	rawUsername := getStringField(userInfo, mapping.Username)
	name := parseSSOAttribute(rawUsername)
	if customName := getStringField(userInfo, mapping.Name); customName != "" {
		name = customName
	}

	employeeID := getStringField(userInfo, mapping.EmployeeID)
	uniqueID := getStringField(userInfo, mapping.UniqueID)
	employeeType := getStringField(userInfo, mapping.EmployeeType)

	// Fallback: if email is empty, try extracting the unique English login name (priority en, fallback cn) from rawUsername
	if email == "" {
		email = parseSSOEnglishName(rawUsername)
	}

	if email == "" {
		log.Printf("[OAuth2] No email/username found in userinfo: %v", userInfo)
		redirectToLoginWithError(c, "SSO 未返回用户邮箱或标识信息")
		return
	}

	// Auto-provision or match local user
	var user models.User
	err = models.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		// Create new user via SSO auto-provisioning
		displayName := name
		if displayName == "" {
			displayName = email
		}

		user = models.User{
			Email:        email,
			Name:         displayName,
			EmployeeID:   employeeID,
			UniqueID:     uniqueID,
			EmployeeType: employeeType,
			RegMethod:    "sso",
			IsAdmin:      false, // Admin privileges are managed manually by administrators
			IsActive:     true,
			Password:     "$2a$10$SSO_USER_NO_PASSWORD_LOGIN", // Marker: SSO user, password login disabled
		}
		if err := models.DB.Create(&user).Error; err != nil {
			log.Printf("[OAuth2] Failed to auto-provision user %s: %v", email, err)
			redirectToLoginWithError(c, "SSO 用户自动开通失败")
			return
		}
		log.Printf("[OAuth2] Auto-provisioned new user: %s (%s)", email, displayName)
	} else {
		// Update attributes from IdP if they changed
		updates := map[string]interface{}{}
		if name != "" && name != user.Name {
			updates["name"] = name
			user.Name = name
		}
		if employeeID != "" && employeeID != user.EmployeeID {
			updates["employee_id"] = employeeID
			user.EmployeeID = employeeID
		}
		if uniqueID != "" && uniqueID != user.UniqueID {
			updates["unique_id"] = uniqueID
			user.UniqueID = uniqueID
		}
		if employeeType != "" && employeeType != user.EmployeeType {
			updates["employee_type"] = employeeType
			user.EmployeeType = employeeType
		}
		if len(updates) > 0 {
			models.DB.Model(&user).Updates(updates)
			log.Printf("[OAuth2] Updated info for user %s: %+v", email, updates)
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
		Email:    user.Email,
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

// fetchUserInfo calls the custom OAuth2 UserInfo endpoint using POST with client info.
func fetchUserInfo(userInfoURL, accessToken string) (map[string]interface{}, error) {
	oauth2Cfg := models.AppConfig.Auth.OAuth2

	// 1. Build the non-standard POST JSON request body
	requestBody, err := json.Marshal(map[string]string{
		"client_id":    oauth2Cfg.ClientID,
		"access_token": accessToken,
		"scope":        strings.Join(oauth2Cfg.Scopes, " "),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal userinfo request body: %w", err)
	}

	// 2. Create POST request
	req, err := http.NewRequest("POST", userInfoURL, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Also keep the Bearer token in the header in case the IdP does hybrid checks
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

// parseSSOAttribute parses a LDAP-style CN/EN username string (e.g. "cn=傅贵,en=fugui" or "en=fugui")
// and returns cn if present, en if not, or the original string if neither exists.
func parseSSOAttribute(val string) string {
	if val == "" {
		return ""
	}
	// Look for cn=
	if idx := strings.Index(val, "cn="); idx != -1 {
		sub := val[idx+3:]
		if end := strings.IndexAny(sub, ", "); end != -1 {
			return sub[:end]
		}
		return sub
	}
	// Look for en=
	if idx := strings.Index(val, "en="); idx != -1 {
		sub := val[idx+3:]
		if end := strings.IndexAny(sub, ", "); end != -1 {
			return sub[:end]
		}
		return sub
	}
	return val
}

// parseSSOEnglishName parses a LDAP-style CN/EN username string (e.g. "cn=傅贵,en=fugui" or "en=fugui")
// and returns en if present, cn if not, or the original string if neither exists (ideal for unique login ID fallback).
func parseSSOEnglishName(val string) string {
	if val == "" {
		return ""
	}
	// Look for en=
	if idx := strings.Index(val, "en="); idx != -1 {
		sub := val[idx+3:]
		if end := strings.IndexAny(sub, ", "); end != -1 {
			return sub[:end]
		}
		return sub
	}
	// Look for cn=
	if idx := strings.Index(val, "cn="); idx != -1 {
		sub := val[idx+3:]
		if end := strings.IndexAny(sub, ", "); end != -1 {
			return sub[:end]
		}
		return sub
	}
	return val
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
