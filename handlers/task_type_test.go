package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
)

func TestCreateTaskTypeRejectsUnknownEngineMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_task_type_engine_mode", &models.TaskType{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	router := gin.New()
	router.POST("/api/task-types", CreateTaskType)
	body, _ := json.Marshal(map[string]any{
		"name":         "unknown-engine",
		"display_name": "Unknown Engine",
		"engine_mode":  "not_a_real_engine",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var count int64
	db.Model(&models.TaskType{}).Where("name = ?", "unknown-engine").Count(&count)
	if count != 0 {
		t.Fatalf("invalid task type persisted, count=%d", count)
	}
}

func TestCreateTaskTypeRejectsInvalidScanProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_task_type_invalid_profile", &models.TaskType{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	router := gin.New()
	router.POST("/api/task-types", CreateTaskType)
	body, _ := json.Marshal(map[string]any{
		"name":         "invalid-profile",
		"display_name": "Invalid Profile",
		"engine_mode":  "debate_full",
		"engine_config": map[string]any{
			"scan_profile": map[string]any{"version": 1, "name": "custom"},
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var count int64
	db.Model(&models.TaskType{}).Where("name = ?", "invalid-profile").Count(&count)
	if count != 0 {
		t.Fatalf("invalid profile task type persisted, count=%d", count)
	}
}

func TestCreateTaskTypeCanonicalizesScanProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_task_type_canonical_profile", &models.TaskType{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	router := gin.New()
	router.POST("/api/task-types", CreateTaskType)
	body, _ := json.Marshal(map[string]any{
		"name":         "canonical-profile",
		"display_name": "Canonical Profile",
		"engine_mode":  "debate_full",
		"engine_config": map[string]any{
			"scan_profile": map[string]any{
				"version":          1,
				"name":             "keyword_review",
				"target_scope":     "business",
				"content_keywords": []string{"pthread_create"},
			},
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var response models.TaskType
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ScanProfileHash == "" {
		t.Fatal("create response has empty scan_profile_hash")
	}
	var stored models.TaskType
	if err := db.Where("name = ?", "canonical-profile").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	var config struct {
		ScanProfile struct {
			PrimaryUnit string `json:"primary_unit"`
		} `json:"scan_profile"`
	}
	if err := json.Unmarshal(stored.EngineConfig, &config); err != nil {
		t.Fatal(err)
	}
	if config.ScanProfile.PrimaryUnit != "file" {
		t.Fatalf("canonical primary_unit = %q, want file", config.ScanProfile.PrimaryUnit)
	}
}

func TestUpdateTaskTypeRejectsStaleUpdatedAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, "shield_task_type_profile_optimistic_lock", &models.TaskType{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	taskType := models.TaskType{
		Name:         "stale-profile",
		DisplayName:  "Stale Profile",
		EngineMode:   "debate_full",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review"}}`),
		TargetScope:  "business",
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.PATCH("/api/task-types/:id", UpdateTaskType)
	body, _ := json.Marshal(map[string]any{
		"display_name": "Updated Elsewhere",
		"updated_at":   taskType.UpdatedAt.Add(-time.Minute).Format(time.RFC3339Nano),
	})
	request := httptest.NewRequest(http.MethodPatch, "/api/task-types/1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var unchanged models.TaskType
	if err := db.Where("name = ?", "stale-profile").First(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.DisplayName != "Stale Profile" {
		t.Fatalf("stale request changed task type: %q", unchanged.DisplayName)
	}
}
