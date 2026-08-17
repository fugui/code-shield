package handlers

import (
	commonAuth "code-common/backend/auth"
	"code-shield/models"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type PortalClaims = commonAuth.PortalClaims

var oauth2States *commonAuth.StateStore

func init() {
	oauth2States = commonAuth.NewStateStore()
}

func parseToken(tokenString string) (*PortalClaims, error) {
	return commonAuth.ParseToken(tokenString, models.AppConfig.Auth.JWTSecret)
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := commonAuth.ExtractToken(c)
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header or token missing"})
			c.Abort()
			return
		}

		claims, err := parseToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token signature"})
			c.Abort()
			return
		}

		var user models.User
		if err := models.DB.First(&user, claims.UserID).Error; err != nil {
			// Auto-register shadow user in Code Shield DB to keep IDs aligned
			email := claims.Email
			if email == "" {
				email = claims.Username
			}

			var existingUser models.User
			if errEmail := models.DB.Where("email = ?", email).First(&existingUser).Error; errEmail == nil {
				// 账号 ID 未对齐，进行主键对齐和关联关系级联更新
				oldID := existingUser.ID
				newID := claims.UserID
				errTx := models.DB.Transaction(func(tx *gorm.DB) error {
					if err := tx.Exec("UPDATE users SET id = ?, reg_method = 'sso', is_active = 1 WHERE id = ?", newID, oldID).Error; err != nil {
						return err
					}
					tx.Exec("UPDATE departments SET leader_id = ? WHERE leader_id = ?", newID, oldID)
					tx.Exec("UPDATE repositories SET owner_id = ? WHERE owner_id = ?", newID, oldID)
					tx.Exec("UPDATE test_case_findings SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					tx.Exec("UPDATE coredump_findings SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					tx.Exec("UPDATE float_findings SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					tx.Exec("UPDATE thread_findings SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					tx.Exec("UPDATE cjson_findings SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					tx.Exec("UPDATE key_issues SET assignee_id = ? WHERE assignee_id = ?", newID, oldID)
					return nil
				})
				if errTx != nil {
					log.Printf("[Auth] Failed to align user ID from %d to %d for email %s: %v", oldID, newID, email, errTx)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO user ID alignment failed"})
					c.Abort()
					return
				}
				log.Printf("[Auth] Aligned user ID from %d to %d for email %s and updated relations", oldID, newID, email)
				user = existingUser
				user.ID = newID
				user.RegMethod = "sso"
				user.IsActive = true
			} else {
				name := claims.Name
				if name == "" {
					name = email
				}
				user = models.User{
					ID:        claims.UserID,
					Email:     email,
					Name:      name,
					IsActive:  true,
					RegMethod: "sso",
					Password:  "$2a$10$SSO_USER_NO_PASSWORD_LOGIN",
				}
				if err := models.DB.Create(&user).Error; err != nil {
					log.Printf("[Auth] Failed to auto-provision user ID %d: %v", claims.UserID, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO user auto-provisioning failed"})
					c.Abort()
					return
				}
				log.Printf("[Auth] Auto-provisioned shadow user ID %d (%s)", user.ID, user.Email)
			}
		} else {
			// Update admin status or name from token if changed
			updates := map[string]interface{}{}
			if claims.Name != "" && claims.Name != user.Name {
				updates["name"] = claims.Name
				user.Name = claims.Name
			}
			if user.RegMethod == "imported" {
				updates["reg_method"] = "sso"
				user.RegMethod = "sso"
			}
			if !user.IsActive {
				updates["is_active"] = true
				user.IsActive = true
			}
			if len(updates) > 0 {
				models.DB.Model(&user).Updates(updates)
			}
		}

		effectiveRoles := claims.Roles
		dbRoles := user.GetRoles()
		if len(dbRoles) > 0 {
			effectiveRoles = dbRoles
		}

		c.Set("userID", user.ID)
		c.Set("username", user.Email)
		c.Set("email", user.Email)
		c.Set("roles", effectiveRoles)
		c.Next()
	}
}

func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("userID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		var user models.User
		if err := models.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}

		rolesVal, rolesExists := c.Get("roles")
		hasRole := false
		if rolesExists {
			if roles, ok := rolesVal.([]string); ok {
				for _, r := range roles {
					if r == "super_admin" || r == "shield_admin" {
						hasRole = true
						break
					}
				}
			}
		}

		if !user.HasRole("shield_admin") && !hasRole {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin privileges required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func GetMe(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}

	var user models.User
	if err := models.DB.Preload("Department").First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	rolesVal, _ := c.Get("roles")
	roles, _ := rolesVal.([]string)

	c.JSON(http.StatusOK, gin.H{
		"id":            user.ID,
		"email":         user.Email,
		"name":          user.Name,
		"is_admin":      user.IsAdmin,
		"is_active":     user.IsActive,
		"roles":         roles,
		"department_id": user.DepartmentID,
		"department":    user.Department,
	})
}

func UpdatePassword(c *gin.Context) {
	authCfg := models.AppConfig.Auth
	if !authCfg.StandaloneMode && !authCfg.PasswordLoginEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请在 CodeBench 主控制台修改您的密码！"})
		return
	}

	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID missing"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := models.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前密码不正确"})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	if err := models.DB.Model(&user).Update("password", string(hashed)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

func UpdateMyDepartment(c *gin.Context) {
	if !models.AppConfig.Auth.StandaloneMode {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请在 CodeBench 主控制台绑定您的归属部门！"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "部门更新成功"})
}

func Login(c *gin.Context) {
	authCfg := models.AppConfig.Auth
	if !authCfg.StandaloneMode && !authCfg.PasswordLoginEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "本地直接登录已停用，请使用主门户登录。"})
		return
	}

	var req struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	loginIdentifier := strings.ToLower(strings.TrimSpace(req.Email))
	if loginIdentifier == "" {
		loginIdentifier = strings.ToLower(strings.TrimSpace(req.Username))
	}
	if loginIdentifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入邮箱或用户名"})
		return
	}

	var user models.User
	if err := models.DB.Where("LOWER(email) = LOWER(?) OR LOWER(username) = LOWER(?)", loginIdentifier, loginIdentifier).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号已被禁用"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	token, err := commonAuth.GenerateToken(
		user.ID,
		user.Email,
		user.Email,
		user.Name,
		user.IsSuperAdmin(),
		user.GetRoles(),
		models.AppConfig.Auth.JWTSecret,
		6*time.Hour,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token 生成失败"})
		return
	}

	now := time.Now()
	clientIP := c.ClientIP()
	models.DB.Model(&user).Updates(map[string]interface{}{
		"last_login": now,
		"last_ip":    clientIP,
	})

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

func GetAuthConfig(c *gin.Context) {
	authCfg := models.AppConfig.Auth
	// 在独立模式或者明确开启密码登录时，暴露相应登录能力
	passwordEnabled := authCfg.StandaloneMode || authCfg.PasswordLoginEnabled
	oauth2Enabled := authCfg.OAuth2.Enabled

	c.JSON(http.StatusOK, gin.H{
		"oauth2_enabled":         oauth2Enabled,
		"password_login_enabled": passwordEnabled,
		"dept_api_url":           authCfg.OAuth2.DeptAPIURL,
	})
}

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

func OAuth2Callback(c *gin.Context) {
	oauth2Cfg := models.AppConfig.Auth.OAuth2
	if !oauth2Cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth2 SSO is not enabled"})
		return
	}

	if errMsg := c.Query("error"); errMsg != "" {
		errDesc := c.Query("error_description")
		log.Printf("[OAuth2] IdP returned error: %s - %s", errMsg, errDesc)
		redirectWithSSOError(c, "SSO 登录失败: "+errDesc)
		return
	}

	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		redirectWithSSOError(c, "SSO 回调参数缺失")
		return
	}

	codeVerifier, ok := oauth2States.ValidateAndConsume(state)
	if !ok {
		redirectWithSSOError(c, "SSO 登录超时或状态无效，请重试")
		return
	}

	tokenData, err := commonAuth.ExchangeCodeForToken(oauth2Cfg, code, codeVerifier)
	if err != nil {
		log.Printf("[OAuth2] Token exchange failed: %v", err)
		redirectWithSSOError(c, "SSO Token 交换失败")
		return
	}

	accessToken, _ := tokenData["access_token"].(string)
	if accessToken == "" {
		redirectWithSSOError(c, "SSO 未返回有效的 access_token")
		return
	}

	userInfo, err := commonAuth.FetchUserInfo(oauth2Cfg.UserInfoURL, oauth2Cfg.ClientID, oauth2Cfg.Scopes, accessToken)
	if err != nil {
		log.Printf("[OAuth2] UserInfo fetch failed: %v", err)
		redirectWithSSOError(c, "SSO 用户信息获取失败")
		return
	}

	mapping := oauth2Cfg.FieldMapping
	email := strings.ToLower(strings.TrimSpace(commonAuth.GetStringField(userInfo, mapping.Email)))
	rawUsername := commonAuth.GetStringField(userInfo, mapping.Username)
	name := commonAuth.ParseSSOAttribute(rawUsername)
	if customName := commonAuth.GetStringField(userInfo, mapping.Name); customName != "" {
		name = customName
	}
	employeeID := strings.TrimSpace(commonAuth.GetStringField(userInfo, mapping.EmployeeID))
	uniqueID := strings.TrimSpace(commonAuth.GetStringField(userInfo, mapping.UniqueID))
	employeeType := strings.TrimSpace(commonAuth.GetStringField(userInfo, mapping.EmployeeType))

	if email == "" {
		email = strings.ToLower(strings.TrimSpace(commonAuth.ParseSSOEnglishName(rawUsername)))
	}
	if email == "" {
		redirectWithSSOError(c, "SSO 未返回用户邮箱或标识信息")
		return
	}

	if !commonAuth.IsEmailDomainAllowed(email, oauth2Cfg.AllowedEmailDomains) {
		var count int64
		if err := models.DB.Model(&models.User{}).Where("LOWER(email) = LOWER(?)", email).Count(&count).Error; err != nil || count == 0 {
			redirectWithSSOError(c, "邮箱域名未被允许，请联系系统管理员")
			return
		}
	}

	isAdmin := false
	for _, adminEmail := range oauth2Cfg.AdminList {
		if strings.EqualFold(strings.TrimSpace(adminEmail), strings.TrimSpace(email)) {
			isAdmin = true
			break
		}
	}

	var user models.User
	userFound := false
	if uniqueID != "" {
		if err := models.DB.Where("unique_id = ?", uniqueID).First(&user).Error; err == nil {
			userFound = true
		}
	}
	if !userFound && email != "" {
		if err := models.DB.Where("LOWER(email) = LOWER(?)", email).First(&user).Error; err == nil {
			userFound = true
		}
	}

	if !userFound {
		displayName := name
		if displayName == "" {
			displayName = email
		}
		var uniqueIDPtr *string
		if uniqueID != "" {
			uniqueIDPtr = &uniqueID
		}
		var initialRoles datatypes.JSON
		if isAdmin {
			b, _ := commonAuth.GenerateToken(0, "", "", "", false, nil, "", 0) // dummy
			_ = b
			initialRoles = datatypes.JSON(`["super_admin", "shield_admin"]`)
		}
		user = models.User{
			Email:        email,
			Name:         displayName,
			EmployeeID:   employeeID,
			UniqueID:     uniqueIDPtr,
			EmployeeType: employeeType,
			RegMethod:    "sso",
			Roles:        initialRoles,
			IsActive:     true,
			Password:     "$2a$10$SSO_USER_NO_PASSWORD_LOGIN",
		}
		if err := models.DB.Create(&user).Error; err != nil {
			log.Printf("[OAuth2] Failed to auto-provision user: %v", err)
			redirectWithSSOError(c, "SSO 用户自动创建失败")
			return
		}
	}

	now := time.Now()
	models.DB.Model(&user).Updates(map[string]interface{}{
		"last_login": now,
		"last_ip":    c.ClientIP(),
	})

	tokenString, err := commonAuth.GenerateToken(
		user.ID,
		user.Email,
		user.Email,
		user.Name,
		user.IsSuperAdmin(),
		user.GetRoles(),
		models.AppConfig.Auth.JWTSecret,
		6*time.Hour,
	)
	if err != nil {
		redirectWithSSOError(c, "登录凭证生成失败")
		return
	}

	externalURL := strings.TrimRight(models.AppConfig.Server.ExternalURL, "/")
	redirectTarget := externalURL + "/?token=" + url.QueryEscape(tokenString)
	c.Redirect(http.StatusFound, redirectTarget)
}

func redirectWithSSOError(c *gin.Context, errorMsg string) {
	externalURL := strings.TrimRight(models.AppConfig.Server.ExternalURL, "/")
	loginURL := externalURL + "/login?sso_error=" + url.QueryEscape(errorMsg)
	c.Redirect(http.StatusFound, loginURL)
}
