package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware_AutomaticRenewal(t *testing.T) {
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
