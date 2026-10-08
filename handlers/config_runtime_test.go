package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"code-shield/models"
	"code-shield/services"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupConfigRuntimeTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/config.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemConfig{}, &models.SystemDynamicConfig{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	originalDB := models.DB
	originalAppConfig := models.AppConfig
	originalDispatcher := services.Dispatcher
	t.Cleanup(func() {
		models.DB = originalDB
		models.AppConfig = originalAppConfig
		services.Dispatcher = originalDispatcher
	})

	models.DB = db
	services.Dispatcher = &services.ModelDispatcher{}
}

func scannerRuntimeConfig(enabled bool) models.ScannerConfig {
	return models.ScannerConfig{
		Throttling: models.ThrottlingConfig{
			WorkHours: models.WorkHoursConfig{
				Enabled:   enabled,
				Workdays:  []int{1, 2, 3, 4, 5},
				StartTime: "09:00",
				EndTime:   "18:00",
				Scale:     0.25,
			},
		},
	}
}

func assertSyncedWorkHours(t *testing.T, expected models.WorkHoursConfig) {
	t.Helper()
	info := services.Dispatcher.GetThrottleInfo()
	if !reflect.DeepEqual(info.WorkHoursConfig, expected) {
		t.Fatalf("expected dispatcher config %+v, got %+v", expected, info.WorkHoursConfig)
	}
}

func TestUpdateCategoryConfig_SyncsWorkHoursThrottle(t *testing.T) {
	setupConfigRuntimeTest(t)

	body, err := json.Marshal(scannerRuntimeConfig(true))
	if err != nil {
		t.Fatalf("marshal scanner config: %v", err)
	}
	router := gin.New()
	router.PUT("/category/:category", UpdateCategoryConfig)

	request := httptest.NewRequest(http.MethodPut, "/category/scanner", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	assertSyncedWorkHours(t, models.AppConfig.Scanner.Throttling.WorkHours)
}

func TestUpdateFullConfig_SyncsWorkHoursThrottle(t *testing.T) {
	setupConfigRuntimeTest(t)

	body, err := json.Marshal(map[string]models.ScannerConfig{
		"scanner": scannerRuntimeConfig(true),
	})
	if err != nil {
		t.Fatalf("marshal full config: %v", err)
	}
	router := gin.New()
	router.PUT("/full", UpdateFullConfig)

	request := httptest.NewRequest(http.MethodPut, "/full", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	assertSyncedWorkHours(t, models.AppConfig.Scanner.Throttling.WorkHours)
}

func TestUpdateFullConfig_RejectsMissingTierResource(t *testing.T) {
	setupConfigRuntimeTest(t)

	scanner := scannerRuntimeConfig(true)
	scanner.Debate.Tiers.Tier1Hunter.Resource = "codex-glm5.3"
	scanner.Debate.Tiers.Tier1Hunter.Resources = []string{"codex-glm5.3"}

	body, err := json.Marshal(map[string]models.ScannerConfig{"scanner": scanner})
	if err != nil {
		t.Fatalf("marshal scanner config: %v", err)
	}
	router := gin.New()
	router.PUT("/full", UpdateFullConfig)

	request := httptest.NewRequest(http.MethodPut, "/full", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter.Resource == "codex-glm5.3" {
		t.Fatal("invalid scanner config was applied")
	}
}
