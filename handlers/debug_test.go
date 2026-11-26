package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAuth "code-common/backend/auth"
	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
)

func TestDailyStatsAggregatesDisplayedMetrics(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_debug_daily_stats",
		&models.TaskReport{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	today := time.Now()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	yesterday := todayStart.Add(-24 * time.Hour)

	statuses := []string{
		models.StatusSuccess,
		models.StatusFailed,
		models.StatusDegraded,
		models.StatusSkipped,
		models.StatusQueued,
	}
	for _, status := range statuses {
		report := models.TaskReport{Status: status, Tier1Tokens: 10, Tier2Tokens: 20}
		if status == models.StatusQueued {
			report.CreatedAt = yesterday
		}
		if err := db.Create(&report).Error; err != nil {
			t.Fatalf("create %s report: %v", status, err)
		}
	}
	stats := loadDailyStats(db, todayStart)
	if stats.Total != 4 || stats.Success != 2 || stats.Failed != 1 {
		t.Fatalf("daily stats = %+v", stats)
	}
	if stats.Tier1Tokens != 40 || stats.Tier2Tokens != 80 {
		t.Fatalf("token stats = %+v", stats)
	}
}

func TestDebugLeaseRoutesAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
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

	admin := r.Group("/api")
	admin.Use(commonAuth.RequireAdmin(commonAuth.RoleShieldAdmin))
	RegisterDebugRoutes(admin.Group("/admin/debug"))

	endpoints := []struct {
		method string
		path   string
		body   string
		okCode int
	}{
		{method: http.MethodGet, path: "/api/admin/debug/leases/recent", okCode: http.StatusOK},
		{method: http.MethodPost, path: "/api/admin/debug/leases/recent-options", body: `{"record_successful_leases":true}`, okCode: http.StatusServiceUnavailable},
		{method: http.MethodGet, path: "/api/admin/debug/leases/test-lease", okCode: http.StatusNotFound},
		{method: http.MethodGet, path: "/api/admin/debug/leases/test-lease/prompt", okCode: http.StatusNotFound},
		{method: http.MethodGet, path: "/api/admin/debug/leases/test-lease/console", okCode: http.StatusNotFound},
	}

	roles := []struct {
		name       string
		role       string
		authorized bool
		deniedCode int
	}{
		{name: "unauthenticated", role: "", authorized: false, deniedCode: http.StatusUnauthorized},
		{name: "developer", role: "developer", authorized: false, deniedCode: http.StatusForbidden},
		{name: "shield_admin", role: commonAuth.RoleShieldAdmin, authorized: true},
		{name: "super_admin", role: commonAuth.RoleSuperAdmin, authorized: true},
	}

	for _, ep := range endpoints {
		for _, rc := range roles {
			t.Run(rc.name+"_"+ep.method+"_"+ep.path, func(t *testing.T) {
				var bodyReader *bytes.Reader
				if ep.body != "" {
					bodyReader = bytes.NewReader([]byte(ep.body))
				} else {
					bodyReader = bytes.NewReader(nil)
				}
				req := httptest.NewRequest(ep.method, ep.path, bodyReader)
				if ep.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				if rc.role != "" {
					req.Header.Set("X-Test-Role", rc.role)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				expected := ep.okCode
				if !rc.authorized {
					expected = rc.deniedCode
				}
				if w.Code != expected {
					t.Fatalf("role=%q %s %s: expected status %d, got %d (body=%s)", rc.role, ep.method, ep.path, expected, w.Code, w.Body.String())
				}
			})
		}
	}
}
