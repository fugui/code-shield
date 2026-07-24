package handlers

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type PortalClaims struct {
	UserID   uint     `json:"user_id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	IsAdmin  bool     `json:"is_admin"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

func parseToken(tokenString string) (*PortalClaims, error) {
	secret := []byte(models.AppConfig.Auth.JWTSecret)
	claims := &PortalClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// AuthMiddleware to extract and verify user token issued by code-bench
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing"})
			c.Abort()
			return
		}

		if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
			tokenString = tokenString[7:]
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
					ID:        claims.UserID, // Use the exact same ID as code-bench!
					Email:     email,
					Name:      name,
					IsAdmin:   claims.IsAdmin,
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
			if claims.IsAdmin != user.IsAdmin {
				updates["is_admin"] = claims.IsAdmin
				user.IsAdmin = claims.IsAdmin
			}
			rolesJSON, _ := json.Marshal(claims.Roles)
			if string(user.Roles) != string(rolesJSON) {
				updates["roles"] = datatypes.JSON(rolesJSON)
				user.Roles = datatypes.JSON(rolesJSON)
			}
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

		c.Set("userID", user.ID)
		c.Set("username", user.Email)
		c.Set("email", user.Email)
		c.Set("roles", claims.Roles)
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

		if !user.IsAdmin && !hasRole {
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
	c.JSON(http.StatusBadRequest, gin.H{"error": "请在 CodeBench 主控制台修改您的密码！"})
}

func UpdateMyDepartment(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "请在 CodeBench 主控制台绑定您的归属部门！"})
}

func Login(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "本地直接登录已停用，请使用主门户登录。"})
}

func GetAuthConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"oauth2_enabled":         false,
		"password_login_enabled": false,
		"dept_api_url":           "",
	})
}
