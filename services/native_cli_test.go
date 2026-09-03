package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"code-shield/models"
)

// MockInvoker 用于断路器降级测试中的模拟 CLI
type MockInvoker struct {
	NameStr    string
	InvokedCnt int32
}

func (m *MockInvoker) Name() string {
	return m.NameStr
}

func (m *MockInvoker) Invoke(req AIRequest) error {
	atomic.AddInt32(&m.InvokedCnt, 1)
	if req.OutputPath != "" {
		if err := os.WriteFile(req.OutputPath, []byte(`{"findings": [], "summary": "mock CLI output"}`), 0644); err != nil {
			return err
		}
	}
	return nil
}

func TestNativeInvoker_BasicSuccess(t *testing.T) {
	// 1. 启动模拟 OpenAI API 服务器
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		resp := map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "glm-4-flash",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"is_same": true}`,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     15,
				"completion_tokens": 8,
				"total_tokens":      23,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// 2. 配置 AppConfig
	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:            server.URL,
		APIKey:             "test-key",
		DefaultModel:       "glm-4-flash",
		Temperature:        0.1,
		ResponseFormatJSON: true,
		MaxRetries:         1,
		RetryBackoffMs:     10,
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "output.json")

	err := invoker.Invoke(AIRequest{
		PromptMsg:  "判断两个问题是否相同",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	// 3. 验证输出文件内容
	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(content) != `{"is_same": true}` {
		t.Fatalf("unexpected content: %s", string(content))
	}

	// 4. 验证请求体解析
	if receivedBody["model"] != "glm-4-flash" {
		t.Errorf("expected model glm-4-flash, got %v", receivedBody["model"])
	}
}

func TestNativeInvoker_FailoverAndRetry(t *testing.T) {
	var server1Hits int32
	var server2Hits int32

	// Server 1 总是返回 500
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server1Hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"error": "internal server error"}`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server1.Close()

	// Server 2 正常返回
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server2Hits, 1)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"status": "ok"}`,
					},
				},
			},
			"usage": map[string]interface{}{"total_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server2.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{
		MaxRetries:     2,
		RetryBackoffMs: 10,
		Endpoints: []models.NativeEndpointConfig{
			{Name: "s1", BaseURL: server1.URL, Model: "m1", Weight: 50},
			{Name: "s2", BaseURL: server2.URL, Model: "m2", Weight: 50},
		},
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "output.json")

	err := invoker.Invoke(AIRequest{
		PromptMsg:  "test failover",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if atomic.LoadInt32(&server1Hits) < 1 {
		t.Errorf("expected server1 to be hit at least once, got %d", server1Hits)
	}
	if atomic.LoadInt32(&server2Hits) < 1 {
		t.Errorf("expected server2 to be hit on failover, got %d", server2Hits)
	}

	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(content) != `{"status": "ok"}` {
		t.Fatalf("unexpected content: %s", string(content))
	}
}

func TestNativeInvoker_CircuitBreakerAndFallback(t *testing.T) {
	// 模拟所有 HTTP 端点均不可用
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer badServer.Close()

	mockCLI := &MockInvoker{NameStr: "mock-fallback"}
	RegisterAIInvoker("mock-fallback", mockCLI)

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Backend = "mock-fallback"
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:        badServer.URL,
		MaxRetries:     1,
		RetryBackoffMs: 5,
	}

	invoker := NewNativeInvoker()
	invoker.failureThreshold = 2 // 连续失败 2 次开启熔断

	tmpOut := filepath.Join(t.TempDir(), "output.json")

	// 第一次调用：Native 失败后自动平滑降级至 mock-fallback
	err := invoker.Invoke(AIRequest{
		PromptMsg:  "test circuit breaker 1",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	if atomic.LoadInt32(&mockCLI.InvokedCnt) != 1 {
		t.Fatalf("expected mockCLI invoked 1 time, got %d", mockCLI.InvokedCnt)
	}

	// 触发第 2 次失败以开启 OPEN 状态
	invoker.recordFailure()
	if invoker.cbState != StateOpen {
		t.Fatalf("expected cbState to be OPEN, got %v", invoker.cbState)
	}

	// 第 3 次调用：断路器为 OPEN，直接走 fallback
	err = invoker.Invoke(AIRequest{
		PromptMsg:  "test circuit breaker direct fallback",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("expected direct fallback to succeed, got %v", err)
	}
	if atomic.LoadInt32(&mockCLI.InvokedCnt) != 2 {
		t.Fatalf("expected mockCLI invoked 2 times, got %d", mockCLI.InvokedCnt)
	}
}

func TestRepairJSON_WithNative(t *testing.T) {
	// 启动模拟修复服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"findings": [{"title": "fixed vulnerability"}]}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.ToolBackends.RepairJSON = "native"
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:        server.URL,
		MaxRetries:     1,
		RetryBackoffMs: 10,
	}

	tempDir := t.TempDir()
	brokenJSONPath := filepath.Join(tempDir, "broken.json")
	// 写入一个有语法错误的 JSON（尾部有多余逗号）
	if err := os.WriteFile(brokenJSONPath, []byte(`{"findings": [{"title": "fixed vulnerability",}],}`), 0644); err != nil {
		t.Fatalf("failed to write broken JSON: %v", err)
	}

	fixedBytes, err := RepairJSON(tempDir, brokenJSONPath, "native")
	if err != nil {
		t.Fatalf("RepairJSON failed: %v", err)
	}

	var parsed struct {
		Findings []struct {
			Title string `json:"title"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(fixedBytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal fixed json: %v", err)
	}
	if len(parsed.Findings) != 1 || parsed.Findings[0].Title != "fixed vulnerability" {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}

	// 确认 .fixed.json 临时文件已被清理
	fixedPath := filepath.Join(tempDir, "broken.fixed.json")
	if _, err := os.Stat(fixedPath); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, but still exists", fixedPath)
	}
}

func TestExtractFeedbackRuleViaNative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role": "assistant",
						"content": `{
							"scope_type": "FILE",
							"pattern": "src/crypto/aes.go",
							"rule_action": "IGNORE",
							"reason": "已由外部硬件安全模块保护"
						}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.ToolBackends.FeedbackExtraction = "native"
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:        server.URL,
		MaxRetries:     1,
		RetryBackoffMs: 10,
	}

	rule, err := ExtractFeedbackRuleViaNative("src/crypto/aes.go", "AESKey = nil", "Hardcoded Key", "采用外部 HSM 密钥管理")
	if err != nil {
		t.Fatalf("ExtractFeedbackRuleViaNative failed: %v", err)
	}
	if rule == nil {
		t.Fatal("expected non-nil rule")
	}
	if rule.Pattern != "src/crypto/aes.go" || rule.Reason != "已由外部硬件安全模块保护" {
		t.Fatalf("unexpected extracted rule: %+v", rule)
	}
}

func TestExecuteSynthesisOnce_Tier3Native(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "# Code-Shield 安全扫描报告\n\n## 综述\n本次分析未发现阻断性高危漏洞。",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Tiers.Tier3Synthesis.Backend = "native"
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:        server.URL,
		MaxRetries:     1,
		RetryBackoffMs: 10,
	}

	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "findings.json")
	reportPath := filepath.Join(tempDir, "report.md")
	if err := os.WriteFile(jsonPath, []byte(`{"findings": []}`), 0644); err != nil {
		t.Fatalf("failed to write findings.json: %v", err)
	}

	ctx := &taskContext{
		reportPath: reportPath,
		codesPath:  tempDir,
		taskType: models.TaskType{
			Name:        "security_audit",
			DisplayName: "安全审计",
			Timeout:     5,
		},
		report: models.TaskReport{ID: 999},
	}

	err := ctx.executeSynthesisOnce(jsonPath, "")
	if err != nil {
		t.Fatalf("executeSynthesisOnce failed: %v", err)
	}

	reportContent, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read report: %v", err)
	}
	if len(reportContent) == 0 {
		t.Fatal("report is empty")
	}
}

func TestNativeInvoker_UnauthorizedFailover(t *testing.T) {
	var authServerHits int32
	var backupServerHits int32

	// Auth Server 返回 401 Unauthorized
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authServerHits, 1)
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error": "invalid api key"}`)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer authServer.Close()

	// Backup Server 正常响应
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupServerHits, 1)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"auth_failover": "success"}`,
					},
				},
			},
			"usage": map[string]interface{}{"total_tokens": 12},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer backupServer.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{
		MaxRetries:     2,
		RetryBackoffMs: 10,
		Endpoints: []models.NativeEndpointConfig{
			{Name: "auth-bad", BaseURL: authServer.URL, APIKey: "bad-key", Model: "m1", Weight: 50},
			{Name: "auth-backup", BaseURL: backupServer.URL, APIKey: "good-key", Model: "m2", Weight: 50},
		},
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "output_auth.json")

	err := invoker.Invoke(AIRequest{
		PromptMsg:  "test auth failover",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if atomic.LoadInt32(&authServerHits) < 1 {
		t.Errorf("expected authServer to be hit, got %d", authServerHits)
	}
	if atomic.LoadInt32(&backupServerHits) < 1 {
		t.Errorf("expected backupServer to be hit on 401 failover, got %d", backupServerHits)
	}

	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(content) != `{"auth_failover": "success"}` {
		t.Fatalf("unexpected content: %s", string(content))
	}
}

func TestNativeInvoker_ResponseFormatDynamic(t *testing.T) {
	var lastReceivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &lastReceivedBody)

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "# Markdown 报告正文\n\n## 一、检视结果概要\n\n正常完成",
					},
				},
			},
			"usage": map[string]interface{}{
				"total_tokens": 100,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	// 1. 全局开启 ResponseFormatJSON=true，但请求显式声明 ResponseFormat="text"
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:            server.URL,
		APIKey:             "test-key",
		DefaultModel:       "glm-4-flash",
		ResponseFormatJSON: true,
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "report.md")

	err := invoker.Invoke(AIRequest{
		PromptMsg:      "生成 Markdown 报告",
		OutputPath:     tmpOut,
		TimeoutMin:     1,
		ResponseFormat: "text",
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if _, exists := lastReceivedBody["response_format"]; exists {
		t.Errorf("expected NO response_format in request body when ResponseFormat='text', got: %v", lastReceivedBody["response_format"])
	}

	// 2. 全局关闭 ResponseFormatJSON=false，但请求显式声明 ResponseFormat="json"
	models.AppConfig.AI.Native.ResponseFormatJSON = false
	err = invoker.Invoke(AIRequest{
		PromptMsg:      "输出结构化 JSON",
		OutputPath:     tmpOut,
		TimeoutMin:     1,
		ResponseFormat: "json",
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	rf, exists := lastReceivedBody["response_format"].(map[string]interface{})
	if !exists || rf["type"] != "json_object" {
		t.Errorf("expected response_format type json_object when ResponseFormat='json', got: %v", lastReceivedBody["response_format"])
	}
}
