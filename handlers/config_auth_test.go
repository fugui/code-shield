package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonAuth "code-common/backend/auth"

	"github.com/gin-gonic/gin"
)

func TestConfigSuperAdminAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 构建复现 main.go 中的 superAdmin 路由组鉴权
	r := gin.New()

	// 模拟 AuthMiddleware 解析 userContext
	r.Use(func(c *gin.Context) {
		roleHeader := c.GetHeader("X-Test-Role")
		if roleHeader != "" {
			commonAuth.SetUserContext(c, &commonAuth.UserContext{
				UserID:   1001,
				Username: "test-user",
				Roles:    []string{roleHeader},
			})
		}
		c.Next()
	})

	superAdmin := r.Group("/api")
	superAdmin.Use(commonAuth.RequireAdmin(commonAuth.RoleSuperAdmin))
	{
		superAdmin.GET("/admin/compute/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		superAdmin.PUT("/admin/compute/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
	}

	tests := []struct {
		name         string
		method       string
		role         string
		path         string
		expectedCode int
	}{
		{
			name:         "未认证用户访问 GET /api/admin/compute/config 返回 401",
			method:       http.MethodGet,
			role:         "",
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "shield_admin 访问 GET /api/admin/compute/config 返回 403",
			method:       http.MethodGet,
			role:         commonAuth.RoleShieldAdmin,
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "developer 访问 GET /api/admin/compute/config 返回 403",
			method:       http.MethodGet,
			role:         "developer",
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "super_admin 访问 GET /api/admin/compute/config 返回 200",
			method:       http.MethodGet,
			role:         commonAuth.RoleSuperAdmin,
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusOK,
		},
		{
			name:         "shield_admin 访问 PUT /api/admin/compute/config 返回 403",
			method:       http.MethodPut,
			role:         commonAuth.RoleShieldAdmin,
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "super_admin 访问 PUT /api/admin/compute/config 返回 200",
			method:       http.MethodPut,
			role:         commonAuth.RoleSuperAdmin,
			path:         "/api/admin/compute/config",
			expectedCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.role != "" {
				req.Header.Set("X-Test-Role", tc.role)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Fatalf("路径 %s %s 权限校验失败: 期望状态码 %d, 实际得到 %d", tc.method, tc.path, tc.expectedCode, w.Code)
			}
		})
	}
}
