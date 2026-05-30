package handlers

import (
	"code-shield/models"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var jwtSecret = []byte("super-secret-key-for-code-shield") // DEPRECATED: kept for test backward compat only, overridden at runtime
var portalJwtSecret = []byte("portal-shared-secret-key")    // DEPRECATED: kept for test backward compat only, overridden at runtime

// getJWTSecret returns the JWT signing key from configuration.
func getJWTSecret() []byte {
	if s := models.AppConfig.Auth.JWTSecret; s != "" {
		return []byte(s)
	}
	return jwtSecret // fallback for tests where config is not loaded
}

// getPortalJWTSecret returns the Portal SSO JWT key from configuration.
// Returns nil if portal JWT is not configured (disables portal token parsing).
func getPortalJWTSecret() []byte {
	if s := models.AppConfig.Auth.PortalJWTSecret; s != "" {
		return []byte(s)
	}
	if models.AppConfig.Auth.JWTSecret != "" {
		// Config is loaded but portal secret is empty — portal SSO disabled
		return nil
	}
	return portalJwtSecret // fallback for tests
}

type Claims struct {
	UserID   uint   `json:"user_id"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

type PortalClaims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

type UnifiedClaims struct {
	UserID   uint
	Email    string
	Name     string
	IsAdmin  bool
}

// Unified parser that validates tokens from both Code-Shield and Portal (SSO)
func parseToken(tokenString string) (*UnifiedClaims, error) {
	secret := getJWTSecret()

	// Try 1: Parse using local Code-Shield secret
	shieldClaims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, shieldClaims, func(token *jwt.Token) (interface{}, error) {
		// Reject 'none' algorithm — hardcode expected algorithm
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err == nil && token.Valid {
		return &UnifiedClaims{
			UserID:   shieldClaims.UserID,
			Email:    shieldClaims.Email,
		}, nil
	}

	// Try 2: Parse using Portal shared secret (if configured)
	portalSecret := getPortalJWTSecret()
	if portalSecret != nil {
		portalClaims := &PortalClaims{}
		token, err = jwt.ParseWithClaims(tokenString, portalClaims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return portalSecret, nil
		})
		if err == nil && token.Valid {
			// Portal SSO maps Username to Email if Email is blank, but we prefer Email
			email := portalClaims.Email
			if email == "" {
				email = portalClaims.Username
			}
			return &UnifiedClaims{
				Email:    email,
				Name:     portalClaims.Name,
				IsAdmin:  portalClaims.IsAdmin,
			}, nil
		}
	}

	return nil, err
}

func Login(c *gin.Context) {
	// Check if password login is enabled
	if !models.AppConfig.Auth.PasswordLoginEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "Password login is disabled. Please use SSO login."})
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := models.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate JWT using config-based secret
	secret := getJWTSecret()
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID:   user.ID,
		Email:    user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Update last_login
	now := time.Now()
	models.DB.Model(&user).Update("last_login", now)
	user.LastLogin = &now // reflect in response

	c.JSON(http.StatusOK, gin.H{
		"token": tokenString,
		"user":  user,
	})
}

// Middleware to extract and verify user token
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing"})
			c.Abort()
			return
		}

		// Remove Bearer prefix if exists
		if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
			tokenString = tokenString[7:]
		}

		// 1. Verify token signature
		unifiedClaims, err := parseToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token signature"})
			c.Abort()
			return
		}

		// 2. Automated user provisioning
		var user models.User
		err = models.DB.Where("email = ?", unifiedClaims.Email).First(&user).Error
		if err != nil {
			// Auto-register user in Code Shield DB if authenticated by SSO Portal
			name := unifiedClaims.Name
			if name == "" {
				name = unifiedClaims.Email
			}

			user = models.User{
				Email:     unifiedClaims.Email,
				Name:      name,
				IsAdmin:   unifiedClaims.IsAdmin,
				IsActive:  true,
				RegMethod: "sso",
				Password:  "$2a$10$wS2/7R1/x0WjG.y2B2P2Xe/r5a1p1o1s1y1s1t1e1m1p1a1s1s1w", // Disable password bypass
			}
			if err := models.DB.Create(&user).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO user auto-provisioning failed"})
				c.Abort()
				return
			}
		}

		// 3. Automatic token renewal — if less than 12h remaining, issue a fresh 24h token
		shieldClaims := &Claims{}
		secret := getJWTSecret()
		parsedToken, parseErr := jwt.ParseWithClaims(tokenString, shieldClaims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return secret, nil
		})
		if parseErr == nil && parsedToken.Valid && shieldClaims.ExpiresAt != nil {
			remaining := time.Until(shieldClaims.ExpiresAt.Time)
			if remaining > 0 && remaining < 12*time.Hour {
				newExp := time.Now().Add(24 * time.Hour)
				newClaims := &Claims{
					UserID:   user.ID,
					Email:    user.Email,
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(newExp),
					},
				}
				newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, newClaims)
				if signed, err := newToken.SignedString(secret); err == nil {
					c.Header("X-Refresh-Token", signed)
					c.Header("Access-Control-Expose-Headers", "X-Refresh-Token")
				}
			}
		}

		// 4. Inject user data into context
		c.Set("userID", user.ID)
		c.Set("username", user.Email)
		c.Set("email", user.Email)
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
	if err := models.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

func UpdatePassword(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
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

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect current password"})
		return
	}

	// Hash new password
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	// Update password
	if err := models.DB.Model(&user).Update("password", string(hashed)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}
