package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTaskTypeTestDB(t *testing.T, name string, modelsToMigrate ...interface{}) *gorm.DB {
	t.Helper()
	if os.Getenv("TEST_DB_DSN") != "" {
		if db := testdb.SetupIsolatedDB(t, name, modelsToMigrate...); db != nil {
			return db
		}
	}
	dbPath := filepath.Join(t.TempDir(), name+".db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite test db: %v", err)
	}
	if err := db.AutoMigrate(modelsToMigrate...); err != nil {
		t.Fatalf("failed to migrate sqlite test db: %v", err)
	}
	return db
}

func TestCreateTaskTypeRejectsUnknownEngineMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTaskTypeTestDB(t, "shield_task_type_engine_mode", &models.TaskType{})
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
	db := setupTaskTypeTestDB(t, "shield_task_type_invalid_profile", &models.TaskType{})
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
	db := setupTaskTypeTestDB(t, "shield_task_type_canonical_profile", &models.TaskType{})
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = oldDB
		_ = os.RemoveAll(models.AppConfig.GetAbsPath("tasks/canonical-profile"))
	})

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

	// 验证磁盘 meta.json 同样具备 scan_profile 外层包装
	metaPath := filepath.Join(models.AppConfig.GetAbsPath(stored.TaskDir()), "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read created meta.json from disk: %v", err)
	}
	var diskMeta struct {
		EngineConfig struct {
			ScanProfile struct {
				Name        string `json:"name"`
				PrimaryUnit string `json:"primary_unit"`
			} `json:"scan_profile"`
		} `json:"engine_config"`
		ScanProfileHash string `json:"scan_profile_hash"`
	}
	if err := json.Unmarshal(metaBytes, &diskMeta); err != nil {
		t.Fatalf("unmarshal disk meta.json failed: %v", err)
	}
	if diskMeta.EngineConfig.ScanProfile.Name != "occurrence_review" {
		t.Fatalf("disk meta scan_profile.name = %q, want occurrence_review", diskMeta.EngineConfig.ScanProfile.Name)
	}
	if diskMeta.EngineConfig.ScanProfile.PrimaryUnit != "file" {
		t.Fatalf("disk meta scan_profile.primary_unit = %q, want file", diskMeta.EngineConfig.ScanProfile.PrimaryUnit)
	}
	if diskMeta.ScanProfileHash != "" {
		t.Fatalf("derived scan_profile_hash should not be persisted on disk: %q", diskMeta.ScanProfileHash)
	}
}

func TestUpdateTaskTypeRejectsStaleUpdatedAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTaskTypeTestDB(t, "shield_task_type_profile_optimistic_lock", &models.TaskType{})
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

func TestUpdateTaskTypeCanonicalizesScanProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTaskTypeTestDB(t, "shield_task_type_update_canonical", &models.TaskType{})
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = oldDB
		_ = os.RemoveAll(models.AppConfig.GetAbsPath("tasks/update-profile"))
	})

	taskType := models.TaskType{
		Name:         "update-profile",
		DisplayName:  "Update Profile",
		EngineMode:   "debate_full",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review"}}`),
		TargetScope:  "business",
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.PATCH("/api/task-types/:id", UpdateTaskType)

	updateRaw := json.RawMessage(`{"scan_profile":{"version":1,"name":"keyword_review","content_keywords":["cJSON_"]}}`)
	body, _ := json.Marshal(map[string]any{
		"engine_config": updateRaw,
	})
	request := httptest.NewRequest(http.MethodPatch, "/api/task-types/1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response models.TaskType
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ScanProfileHash == "" {
		t.Fatal("update response has empty scan_profile_hash")
	}

	var stored models.TaskType
	if err := db.Where("name = ?", "update-profile").First(&stored).Error; err != nil {
		t.Fatal(err)
	}

	var config struct {
		ScanProfile struct {
			Name        string `json:"name"`
			PrimaryUnit string `json:"primary_unit"`
		} `json:"scan_profile"`
	}
	if err := json.Unmarshal(stored.EngineConfig, &config); err != nil {
		t.Fatalf("unmarshal stored engine_config failed: %v", err)
	}
	if config.ScanProfile.Name != "occurrence_review" {
		t.Fatalf("stored profile.name = %q, want occurrence_review", config.ScanProfile.Name)
	}
	if config.ScanProfile.PrimaryUnit != "file" {
		t.Fatalf("stored profile.primary_unit = %q, want file", config.ScanProfile.PrimaryUnit)
	}

	// 验证磁盘 meta.json 同样具备 scan_profile 结构
	metaPath := filepath.Join(models.AppConfig.GetAbsPath(stored.TaskDir()), "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read updated meta.json: %v", err)
	}
	var diskConfig struct {
		EngineConfig struct {
			ScanProfile struct {
				Name string `json:"name"`
			} `json:"scan_profile"`
		} `json:"engine_config"`
	}
	if err := json.Unmarshal(metaBytes, &diskConfig); err != nil {
		t.Fatalf("unmarshal disk meta failed: %v", err)
	}
	if diskConfig.EngineConfig.ScanProfile.Name != "occurrence_review" {
		t.Fatalf("disk meta scan_profile.name = %q, want occurrence_review", diskConfig.EngineConfig.ScanProfile.Name)
	}
}
