package handlers

import (
	"code-shield/models"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for tests
// and seeds it with test users matching the token claims.
func setupTestDB(t *testing.T) {
	t.Helper()
	var err error
	models.DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := models.DB.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	// Seed test users that match token claims below
	models.DB.Create(&models.User{
		Username: "test@user.com",
		Name:     "Test User",
		Password: "$2a$10$placeholder",
		IsActive: true,
	})
	models.DB.Create(&models.User{
		Username: "test2@user.com",
		Name:     "Test User 2",
		Password: "$2a$10$placeholder",
		IsActive: true,
	})
}

func TestAuthMiddleware_AutomaticRenewal(t *testing.T) {
	setupTestDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Case A: Token with 23 hours remaining (should NOT trigger renewal)
	t.Run("TokenNotCloseToExpiration", func(t *testing.T) {
		exp := time.Now().Add(23 * time.Hour)
		claims := &Claims{
			UserID:   1,
			Username: "test@user.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(exp),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, _ := token.SignedString(jwtSecret)

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("X-Refresh-Token"))
	})

	// Case B: Token with 10 hours remaining (should trigger renewal)
	t.Run("TokenCloseToExpiration", func(t *testing.T) {
		exp := time.Now().Add(10 * time.Hour)
		claims := &Claims{
			UserID:   2,
			Username: "test2@user.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(exp),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, _ := token.SignedString(jwtSecret)

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		renewedToken := w.Header().Get("X-Refresh-Token")
		assert.NotEmpty(t, renewedToken)
		assert.Equal(t, "X-Refresh-Token", w.Header().Get("Access-Control-Expose-Headers"))

		// Verify the renewed token is valid and has 24h expiration
		newClaims := &Claims{}
		parsedToken, err := jwt.ParseWithClaims(renewedToken, newClaims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		assert.NoError(t, err)
		assert.True(t, parsedToken.Valid)
		assert.Equal(t, uint(2), newClaims.UserID)
		assert.Equal(t, "test2@user.com", newClaims.Username)
		assert.True(t, time.Until(newClaims.ExpiresAt.Time) > 23*time.Hour)
	})
}

func TestOAuth2StateStore(t *testing.T) {
	store := NewStateStore()

	t.Run("GenerateAndValidate", func(t *testing.T) {
		state, _, codeChallenge, err := store.GenerateState()
		assert.NoError(t, err)
		assert.NotEmpty(t, state)
		assert.NotEmpty(t, codeChallenge)

		// Validate and consume the state
		verifier, ok := store.ValidateAndConsume(state)
		assert.True(t, ok)
		assert.NotEmpty(t, verifier)

		// Second consumption should fail (one-time use)
		_, ok = store.ValidateAndConsume(state)
		assert.False(t, ok)
	})

	t.Run("InvalidState", func(t *testing.T) {
		_, ok := store.ValidateAndConsume("nonexistent-state")
		assert.False(t, ok)
	})

	t.Run("PKCEChallengeFormat", func(t *testing.T) {
		_, _, challenge, err := store.GenerateState()
		assert.NoError(t, err)
		// S256 code_challenge should be base64url encoded SHA256 (43 chars without padding)
		assert.GreaterOrEqual(t, len(challenge), 40)
	})
}
