package handlers

import (
	"code-shield/models"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var jwtSecret = []byte("super-secret-key-for-code-shield") // In production, move to env vars!
var portalJwtSecret = []byte("portal-shared-secret-key")    // Portal shared SSO secret

type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
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
	Username string
	Email    string
	Name     string
	IsAdmin  bool
}

// Unified parser that validates tokens from both Code-Shield and Portal (SSO)
func parseToken(tokenString string) (*UnifiedClaims, error) {
	// Try 1: Parse using local Code-Shield secret
	shieldClaims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, shieldClaims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err == nil && token.Valid {
		return &UnifiedClaims{
			UserID:   shieldClaims.UserID,
			Username: shieldClaims.Username,
		}, nil
	}

	// Try 2: Parse using Portal shared secret
	portalClaims := &PortalClaims{}
	token, err = jwt.ParseWithClaims(tokenString, portalClaims, func(token *jwt.Token) (interface{}, error) {
		return portalJwtSecret, nil
	})
	if err == nil && token.Valid {
		return &UnifiedClaims{
			Username: portalClaims.Username,
			Email:    portalClaims.Email,
			Name:     portalClaims.Name,
			IsAdmin:  portalClaims.IsAdmin,
		}, nil
	}

	return nil, err
}

func Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := models.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
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

	// Generate JWT
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
		err = models.DB.Where("username = ?", unifiedClaims.Username).First(&user).Error
		if err != nil {
			// Auto-register user in Code Shield DB if authenticated by SSO Portal
			email := unifiedClaims.Email
			if email == "" {
				email = unifiedClaims.Username + "@code-shield.com"
			}
			name := unifiedClaims.Name
			if name == "" {
				name = unifiedClaims.Username
			}

			user = models.User{
				Username:  unifiedClaims.Username,
				Name:      name,
				Email:     email,
				IsAdmin:   unifiedClaims.IsAdmin,
				IsActive:  true,
				Password:  "$2a$10$wS2/7R1/x0WjG.y2B2P2Xe/r5a1p1o1s1y1s1t1e1m1p1a1s1s1w", // Disable password bypass
			}
			if err := models.DB.Create(&user).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO user auto-provisioning failed"})
				c.Abort()
				return
			}
		}

		// 3. Inject user data into context
		c.Set("userID", user.ID)
		c.Set("username", user.Username)
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
