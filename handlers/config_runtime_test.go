package handlers

import (
	"bytes"
	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	"code-shield/models"
	"code-shield/services"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupConfigRuntimeTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/config.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&models.SystemConfig{},
		&models.SystemDynamicConfig{},
		&models.SysAuditLog{},
		&models.TaskExecutionLog{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	originalDB := models.DB
	originalAppConfig := models.AppConfig
	originalDispatcher := services.Dispatcher
	t.Cleanup(func() {
		services.ResizeWorkerPool(0)
		_ = commonAudit.Close(context.Background())
		models.DB = originalDB
		models.AppConfig = originalAppConfig
		services.Dispatcher = originalDispatcher
	})

	models.DB = db
	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		QueueCap:    100,
		BatchSize:   1,
		FlushPeriod: 10 * time.Millisecond,
	})
	services.Dispatcher = &services.ModelDispatcher{}
	return db
}

func validComputePayload(version int, apiKey string) UpdateComputeConfigRequest {
	return UpdateComputeConfigRequest{
		Version: version,
		LLM: models.LLMConfig{
			DefaultResource: "native-1",
			Resources: []models.ComputeResourceConfig{
				{
					ID:         "native-1",
					Driver:     "native",
					Concurrent: 10,
					Endpoints: []models.ResourceEndpointConfig{
						{Name: "ep-1", BaseURL: "http://127.0.0.1:8000/chat/completions", APIKey: apiKey, Model: "glm-4-flash", Concurrent: 10},
					},
				},
				{
					ID:         "agy-1",
					Driver:     "agy",
					Concurrent: 5,
				},
			},
		},
		Tiers: models.DebateTiersConfig{
			Tier1Hunter:     models.TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800, AttemptTimeoutSeconds: 900},
			Tier2Challenger: models.TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200, AttemptTimeoutSeconds: 600},
			Tier3Judge:      models.TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800, AttemptTimeoutSeconds: 900},
			Tier4Synthesis:  models.TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200, AttemptTimeoutSeconds: 600},
		},
		WorkerCount: 7,
	}
}

func TestUpdateComputeConfig_RejectsMissingTierResource(t *testing.T) {
	setupConfigRuntimeTest(t)

	payload := validComputePayload(1, "sk-test12345678")
	payload.Tiers.Tier1Hunter.Resource = "non-existent-node"
	payload.Tiers.Tier1Hunter.Resources = []string{"non-existent-node"}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	router := gin.New()
	router.PUT("/admin/compute/config", UpdateComputeConfig)

	req := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateComputeConfig_RejectsEngineTypeMismatch(t *testing.T) {
	setupConfigRuntimeTest(t)

	payload := validComputePayload(1, "sk-test12345678")
	// Tier 1 Hunter 仅允许 Thick Agent，违规绑定 Native
	payload.Tiers.Tier1Hunter.Resource = "native-1"
	payload.Tiers.Tier1Hunter.Resources = []string{"native-1"}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	router := gin.New()
	router.PUT("/admin/compute/config", UpdateComputeConfig)

	req := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for engine mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateComputeConfig_CASOptimisticLockConflict(t *testing.T) {
	db := setupConfigRuntimeTest(t)

	// 在 DB 中预置一条 version = 2 的记录
	now := time.Now()
	initCompute := models.DynamicComputeConfig{
		LLM:         models.LLMConfig{DefaultResource: "native"},
		WorkerCount: 5,
	}
	raw, _ := json.Marshal(initCompute)
	_ = db.Create(&models.SystemDynamicConfig{
		Category:  "compute",
		Data:      datatypes.JSON(raw),
		Version:   2,
		UpdatedBy: "seed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// 客户端提交 stale version = 1
	payload := validComputePayload(1, "sk-test12345678")
	body, _ := json.Marshal(payload)

	router := gin.New()
	router.PUT("/admin/compute/config", UpdateComputeConfig)

	req := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 必须返回 409 Conflict
	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "版本冲突") {
		t.Fatalf("expected version conflict message, got %s", w.Body.String())
	}
}

func TestUpdateComputeConfig_SuccessAndMaskedAuditDiff(t *testing.T) {
	db := setupConfigRuntimeTest(t)

	// 预置 version = 1
	now := time.Now()
	initCompute := models.DynamicComputeConfig{
		LLM: models.LLMConfig{
			DefaultResource: "native-1",
			Resources: []models.ComputeResourceConfig{
				{
					ID:     "native-1",
					Driver: "native",
					Endpoints: []models.ResourceEndpointConfig{
						{Name: "ep-1", BaseURL: "http://127.0.0.1:8000", APIKey: "sk-oldsecret9999", Concurrent: 5},
					},
				},
				{ID: "agy-1", Driver: "agy"},
			},
		},
		Tiers: models.DebateTiersConfig{
			Tier1Hunter:     models.TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800},
			Tier2Challenger: models.TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200},
		},
		WorkerCount: 5,
	}
	raw, _ := json.Marshal(initCompute)
	_ = db.Create(&models.SystemDynamicConfig{
		Category:  "compute",
		Data:      datatypes.JSON(raw),
		Version:   1,
		UpdatedBy: "seed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	router := gin.New()
	router.Use(commonAudit.Middleware("shield"))
	router.Use(func(c *gin.Context) {
		commonAuth.SetUserContext(c, &commonAuth.UserContext{
			UserID:   1,
			Username: "superadmin_alice",
			Roles:    []string{"super_admin"},
		})
		c.Next()
	})
	router.PUT("/admin/compute/config", UpdateComputeConfig)

	secretKey := "sk-supersecretkey-8888"
	payload := validComputePayload(1, secretKey)
	payload.WorkerCount = 15
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// 强制排空异步审计日志队列至 DB
	_ = commonAudit.Close(context.Background())

	// 验证运行时快照生效
	currCompute := models.GetComputeConfig()
	if currCompute.WorkerCount != 15 {
		t.Errorf("expected WorkerCount 15, got %d", currCompute.WorkerCount)
	}
	if models.GetWorkerCount() != 15 {
		t.Errorf("expected GetWorkerCount 15, got %d", models.GetWorkerCount())
	}

	// 验证 DB 记录递增为 version 2 且 updated_by 记录真实操作人
	var dbRecord models.SystemDynamicConfig
	db.Where("category = ?", "compute").First(&dbRecord)
	if dbRecord.Version != 2 {
		t.Errorf("expected DB version 2, got %d", dbRecord.Version)
	}
	if dbRecord.UpdatedBy != "superadmin_alice" {
		t.Errorf("expected UpdatedBy 'superadmin_alice', got %q", dbRecord.UpdatedBy)
	}

	// 验证操作审计日志已记录且 API Key 脱敏
	var auditLog models.SysAuditLog
	if err := db.Where("target_type = ?", "system_dynamic_config").Order("id DESC").First(&auditLog).Error; err != nil {
		t.Fatalf("expected audit log record created: %v", err)
	}
	auditDiff := auditLog.BeforeData + "\n" + auditLog.AfterData + "\n" + auditLog.DiffSummary
	if strings.Contains(auditDiff, secretKey) {
		t.Fatalf("SECURITY VIOLATION: plaintext API key %q leaked into audit diff: %s", secretKey, auditDiff)
	}
	if !strings.Contains(auditDiff, "******") && !strings.Contains(auditDiff, "sk-****") {
		t.Fatalf("expected masked API key ('******' or 'sk-****') in audit diff, got: %s", auditDiff)
	}
}

func TestPingEndpoint_SecurityValidation(t *testing.T) {
	setupConfigRuntimeTest(t)

	router := gin.New()
	router.POST("/admin/compute/ping-endpoint", PingEndpoint)

	tests := []struct {
		name         string
		baseURL      string
		expectedCode int
	}{
		{
			name:         "空 URL 拒绝",
			baseURL:      "",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "非 HTTP/HTTPS 协议拒绝 (file://)",
			baseURL:      "file:///etc/passwd",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "非 HTTP/HTTPS 协议拒绝 (ftp://)",
			baseURL:      "ftp://example.com/test",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "缺少主机名拒绝",
			baseURL:      "http:///relative/path",
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]string{"base_url": tc.baseURL}
			body, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/admin/compute/ping-endpoint", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d: %s", tc.expectedCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestResetComputeConfigToSeed(t *testing.T) {
	db := setupConfigRuntimeTest(t)

	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	// 创建初始 config.yaml 模版种子
	yamlContent := `
llm:
  default_resource: "seed-native"
  resources:
    - id: "seed-native"
      driver: "native"
      endpoints:
        - name: "seed-ep"
          base_url: "http://127.0.0.1:8000"
          model: "seed-model"
          concurrent: 8
scanner:
  worker_count: 8
  debate:
    tiers:
      tier1_hunter:
        resource: "opencode"
        timeout_seconds: 3600
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "test-token"
  report_base_url: "https://example.invalid"
`
	if err := os.WriteFile("config.yaml", []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// 预置已被脏改的 DB compute (version = 5, worker_count = 99)
	now := time.Now()
	dirtyCompute := models.DynamicComputeConfig{
		LLM:         models.LLMConfig{DefaultResource: "dirty"},
		WorkerCount: 99,
	}
	raw, _ := json.Marshal(dirtyCompute)
	_ = db.Create(&models.SystemDynamicConfig{
		Category:  "compute",
		Data:      datatypes.JSON(raw),
		Version:   5,
		UpdatedBy: "dirty_editor",
		CreatedAt: now,
		UpdatedAt: now,
	})

	router := gin.New()
	router.POST("/admin/compute/reset-to-seed", ResetComputeConfigToSeed)

	req := httptest.NewRequest(http.MethodPost, "/admin/compute/reset-to-seed", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// 验证 DB version 递增为 6
	var dbRecord models.SystemDynamicConfig
	db.Where("category = ?", "compute").First(&dbRecord)
	if dbRecord.Version != 6 {
		t.Errorf("expected DB version 6, got %d", dbRecord.Version)
	}

	// 验证运行时快照被重置为 seed 默认值
	curr := models.GetComputeConfig()
	if curr.LLM.DefaultResource != "seed-native" {
		t.Errorf("expected DefaultResource 'seed-native', got %q", curr.LLM.DefaultResource)
	}
	if curr.WorkerCount != 8 {
		t.Errorf("expected WorkerCount 8, got %d", curr.WorkerCount)
	}
}

func TestUpdateComputeConfig_PreservesExistingEndpointAPIKeyWhenBlankOrMasked(t *testing.T) {
	db := setupConfigRuntimeTest(t)

	const existingKey = "sk-real-production-secret-1234"
	now := time.Now()
	initCompute := models.DynamicComputeConfig{
		LLM: models.LLMConfig{
			DefaultResource: "native-1",
			Resources: []models.ComputeResourceConfig{
				{
					ID:         "native-1",
					Driver:     "native",
					Concurrent: 10,
					Endpoints: []models.ResourceEndpointConfig{
						{Name: "ep-1", BaseURL: "http://127.0.0.1:8000/chat/completions", APIKey: existingKey, Model: "glm-4-flash", Concurrent: 10},
					},
				},
				{ID: "agy-1", Driver: "agy", Concurrent: 5},
			},
		},
		Tiers: models.DebateTiersConfig{
			Tier1Hunter:     models.TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800, AttemptTimeoutSeconds: 900},
			Tier2Challenger: models.TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200, AttemptTimeoutSeconds: 600},
		},
		WorkerCount: 5,
	}
	raw, err := json.Marshal(initCompute)
	if err != nil {
		t.Fatalf("marshal initCompute: %v", err)
	}
	if err := db.Create(&models.SystemDynamicConfig{
		Category:  "compute",
		Data:      datatypes.JSON(raw),
		Version:   1,
		UpdatedBy: "seed",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed compute: %v", err)
	}

	router := gin.New()
	router.PUT("/admin/compute/config", UpdateComputeConfig)

	// 1. 提交空字符串 APIKey ("") 及 Native 高级参数，应保留原有真实密钥并持久化高级参数
	blankKeyPayload := validComputePayload(1, "")
	blankKeyPayload.LLM.Resources[0].EnableThinking = true
	blankKeyPayload.LLM.Resources[0].ResponseFormatJSON = true
	blankKeyPayload.LLM.Resources[0].MaxTokens = 65536
	blankKeyPayload.LLM.Resources[0].FirstByteTimeoutSeconds = 120
	blankKeyPayload.LLM.Resources[0].IdleTimeoutSeconds = 180
	blankKeyPayload.LLM.Resources[0].MaxOutputBytes = 131072
	blankKeyPayload.LLM.Resources[0].MaxRetries = 5
	blankKeyPayload.LLM.Resources[0].RetryBackoffMs = 1000

	body1, err := json.Marshal(blankKeyPayload)
	if err != nil {
		t.Fatalf("marshal blankKeyPayload: %v", err)
	}
	req1 := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 for blank api_key update, got %d: %s", w1.Code, w1.Body.String())
	}

	resAfterBlank := models.AppConfig.FindResource("native-1")
	if resAfterBlank == nil || len(resAfterBlank.Endpoints) == 0 {
		t.Fatalf("expected native-1 resource present after update")
	}
	if resAfterBlank.Endpoints[0].APIKey != existingKey {
		t.Fatalf("expected blank api_key to preserve existing key %q, got %q", existingKey, resAfterBlank.Endpoints[0].APIKey)
	}
	if !resAfterBlank.EnableThinking || !resAfterBlank.ResponseFormatJSON || resAfterBlank.MaxTokens != 65536 || resAfterBlank.FirstByteTimeoutSeconds != 120 {
		t.Fatalf("expected native advanced params persisted, got %+v", *resAfterBlank)
	}

	// 2. 提交掩码形式 APIKey ("sk-****1234")，同样应保留原有真实密钥
	maskedPayload := validComputePayload(2, maskAPIKey(existingKey))
	body2, err := json.Marshal(maskedPayload)
	if err != nil {
		t.Fatalf("marshal maskedPayload: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodPut, "/admin/compute/config", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for masked api_key update, got %d: %s", w2.Code, w2.Body.String())
	}
	resAfterMasked := models.AppConfig.FindResource("native-1")
	if resAfterMasked == nil || resAfterMasked.Endpoints[0].APIKey != existingKey {
		t.Fatalf("expected masked api_key to preserve existing key %q, got %+v", existingKey, resAfterMasked)
	}
}
