package invoker

import (
	"encoding/json"
	"fmt"
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

type configuredMockInvoker struct {
	NameStr    string
	InvokedCnt int32
}

func (m *configuredMockInvoker) Name() string { return m.NameStr }
func (m *configuredMockInvoker) Invoke(req AIRequest) error {
	atomic.AddInt32(&m.InvokedCnt, 1)
	if req.OutputPath != "" {
		return os.WriteFile(req.OutputPath, []byte(`{"fallback":"ok"}`), 0644)
	}
	return nil
}

func TestNativeInvokerFallbackMapsNativeAliasToCLIModel(t *testing.T) {
	var receivedModel string
	RegisterAIInvoker("opencode", &modelCapturingMockInvoker{capture: func(req AIRequest) error {
		receivedModel = req.ModelName
		if req.OutputPath != "" {
			return os.WriteFile(req.OutputPath, []byte(`{"fallback":"ok"}`), 0644)
		}
		return nil
	}})

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{
		{ID: "opencode-deepseek", Driver: "opencode", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "modelgate/dp"}}},
		{ID: "native", Driver: "native", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "dp"}}},
	}

	invoker := NewNativeInvoker()
	outputPath := filepath.Join(t.TempDir(), "fallback.json")
	err := invoker.fallbackToCLI(AIRequest{ModelName: "dp", OutputPath: outputPath})
	if err != nil {
		t.Fatalf("fallbackToCLI failed: %v", err)
	}
	if receivedModel != "modelgate/dp" {
		t.Fatalf("expected CLI model modelgate/dp, got %q", receivedModel)
	}
}

type modelCapturingMockInvoker struct {
	capture func(req AIRequest) error
}

func (m *modelCapturingMockInvoker) Name() string { return "opencode" }

func (m *modelCapturingMockInvoker) Invoke(req AIRequest) error {
	return m.capture(req)
}

func TestSelectNonNativeInvokerPrefersConfiguredThickAgent(t *testing.T) {
	RegisterAIInvoker("opencode", &configuredMockInvoker{NameStr: "opencode"})
	RegisterAIInvoker("claude", &configuredMockInvoker{NameStr: "claude"})

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{
		{ID: "native", Driver: "native", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "thin"}}},
		{ID: "opencode-deepseek", Driver: "opencode", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "deepseek"}}},
	}

	if got, _ := selectNonNativeFallbackResource(); got != "opencode" {
		t.Fatalf("expected first configured non-native driver opencode, got %q", got)
	}
}

func TestSelectNonNativeInvokerIgnoresUnregisteredAndNative(t *testing.T) {
	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{
		{ID: "missing-driver", Driver: "missing-driver", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "not-installed"}}},
		{ID: "native", Driver: "native", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "thin"}}},
	}
	if got, _ := selectNonNativeFallbackResource(); got != "" {
		t.Fatalf("expected empty result when configured driver is not registered, got %q", got)
	}
}

func TestNativeInvoker_FallsBackToConfiguredThickAgent(t *testing.T) {
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer badServer.Close()

	fallback := &configuredMockInvoker{NameStr: "opencode"}
	RegisterAIInvoker("opencode", fallback)

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{
		{ID: "opencode-deepseek", Driver: "opencode", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "modelgate/deepseek-v4-flash"}}},
		{ID: "native", Driver: "native", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "deepseek-v4-flash"}}},
	}
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints:      []models.NativeEndpointConfig{{Name: "default", BaseURL: badServer.URL, Model: "deepseek-v4-flash", Concurrent: 20}},
		MaxRetries:     1,
		RetryBackoffMs: 1,
	}

	invoker := NewNativeInvoker()
	invoker.failureThreshold = 100 // Focus on error-path fallback, not circuit opening.
	tmpOut := filepath.Join(t.TempDir(), "fallback.json")

	err := invoker.Invoke(AIRequest{
		PromptMsg:  "native should fail and use configured thick agent",
		OutputPath: tmpOut,
		TimeoutMin: 1,
	})
	if err != nil {
		t.Fatalf("expected thick-agent fallback to succeed, got %v", err)
	}
	if atomic.LoadInt32(&fallback.InvokedCnt) != 1 {
		t.Fatalf("expected configured thick agent invoked once, got %d", atomic.LoadInt32(&fallback.InvokedCnt))
	}
	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"fallback":"ok"}` {
		t.Fatalf("unexpected fallback output: %s", string(content))
	}
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

func TestNativeInvokerResolveEndpointsEmpty(t *testing.T) {
	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{}
	invoker := NewNativeInvoker()
	if endpoints := invoker.resolveEndpoints("glm-4-flash"); len(endpoints) != 0 {
		t.Fatalf("expected empty endpoints, got %+v", endpoints)
	}
}

func TestNativeInvokerTemperatureFallbackChain(t *testing.T) {
	var temperatures []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		temperature, ok := body["temperature"]
		if ok {
			temperatures = append(temperatures, temperature)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			Model:      "glm-4-flash",
			Concurrent: 1,
		}},
		MaxRetries: 1,
	}
	models.AppConfig.Scanner.Determinism.Enabled = true
	models.AppConfig.Scanner.Determinism.Temperature = 0.2

	invoker := NewNativeInvoker()
	requestTemperature := 0.1
	if err := invoker.Invoke(AIRequest{PromptMsg: "explicit", Temperature: &requestTemperature, OutputPath: filepath.Join(t.TempDir(), "explicit.txt"), TimeoutMin: 1}); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if err := invoker.Invoke(AIRequest{PromptMsg: "deterministic", OutputPath: filepath.Join(t.TempDir(), "deterministic.txt"), TimeoutMin: 1}); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	models.AppConfig.Scanner.Determinism.Enabled = false
	if err := invoker.Invoke(AIRequest{PromptMsg: "server default", OutputPath: filepath.Join(t.TempDir(), "default.txt"), TimeoutMin: 1}); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if len(temperatures) != 2 {
		t.Fatalf("expected deterministic temperatures only, got %+v", temperatures)
	}
	if temperatures[0] != 0.1 || temperatures[1] != 0.2 {
		t.Fatalf("expected [0.1 0.2], got %+v", temperatures)
	}
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
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "glm-4-flash",
			Concurrent: 20,
		}},
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
			{Name: "s1", BaseURL: server1.URL, Model: "m1", Concurrent: 50},
			{Name: "s2", BaseURL: server2.URL, Model: "m2", Concurrent: 50},
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
		Endpoints:      []models.NativeEndpointConfig{{Name: "default", BaseURL: badServer.URL, Model: "glm-4-flash", Concurrent: 20}},
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
			{Name: "auth-bad", BaseURL: authServer.URL, APIKey: "bad-key", Model: "m1", Concurrent: 50},
			{Name: "auth-backup", BaseURL: backupServer.URL, APIKey: "good-key", Model: "m2", Concurrent: 50},
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
		lastReceivedBody = nil
		_ = json.Unmarshal(bodyBytes, &lastReceivedBody)

		if stream, _ := lastReceivedBody["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"# Markdown 报告正文\n\n## 一、检视结果概要\n\n正常完成"}}],"usage":{"total_tokens":100}}`)
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

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
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "glm-4-flash",
			Concurrent: 20,
		}},
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

func TestNativeInvoker_ThinkingControl(t *testing.T) {
	var lastReceivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		lastReceivedBody = nil
		_ = json.Unmarshal(bodyBytes, &lastReceivedBody)

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{"ok":true}`}},
			},
			"usage": map[string]interface{}{"total_tokens": 1},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	// Existing dynamic configs omit enable_thinking; the zero value must disable
	// unbounded reasoning for reasoning-style OpenAI-compatible models.
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "deepseek-v4-flash",
			Concurrent: 20,
		}},
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "thinking.json")
	if err := invoker.Invoke(AIRequest{PromptMsg: "disable thinking", OutputPath: tmpOut, TimeoutMin: 1}); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	thinking, ok := lastReceivedBody["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("expected thinking={type:disabled}, got %#v", lastReceivedBody["thinking"])
	}
	chatTemplate, ok := lastReceivedBody["chat_template_kwargs"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected chat_template_kwargs object, got %#v", lastReceivedBody["chat_template_kwargs"])
	}
	for _, key := range []string{"thinking", "enable_thinking"} {
		if value, exists := chatTemplate[key]; !exists || value != false {
			t.Fatalf("expected chat_template_kwargs.%s=false, got %#v", key, chatTemplate)
		}
	}

	// Models/providers that do not support the thinking field can explicitly
	// restore the old wire format.
	models.AppConfig.AI.Native.EnableThinking = true
	t.Logf("config before second invoke: %#v", models.AppConfig.AI.Native)
	if err := invoker.Invoke(AIRequest{PromptMsg: "provider default thinking", OutputPath: tmpOut, TimeoutMin: 1}); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	t.Logf("second body: %#v", lastReceivedBody)
	if _, exists := lastReceivedBody["thinking"]; exists {
		t.Fatalf("expected no thinking field when EnableThinking=true, got %#v", lastReceivedBody["thinking"])
	}
	if _, exists := lastReceivedBody["chat_template_kwargs"]; exists {
		t.Fatalf("expected no chat_template_kwargs when EnableThinking=true, got %#v", lastReceivedBody["chat_template_kwargs"])
	}
}

func TestNativeInvoker_JSONModeStreams(t *testing.T) {
	var lastReceivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		lastReceivedBody = nil
		_ = json.Unmarshal(bodyBytes, &lastReceivedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"{\"is_same\":"}}]}`)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"true}"}}],"usage":{"total_tokens":23}}`)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "glm-4-flash",
			Concurrent: 20,
		}},
		ResponseFormatJSON: true,
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

	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(content) != `{"is_same":true}` {
		t.Fatalf("unexpected content: %s", string(content))
	}

	if stream, _ := lastReceivedBody["stream"].(bool); !stream {
		t.Errorf("expected stream=true in request body for JSON mode, got: %v", lastReceivedBody["stream"])
	}
	rf, ok := lastReceivedBody["response_format"].(map[string]interface{})
	if !ok || rf["type"] != "json_object" {
		t.Errorf("expected response_format json_object, got: %v", lastReceivedBody["response_format"])
	}
}

func TestNativeInvoker_StreamSurvivesAttemptTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟模型思考 1.5 秒后才开始吐字：超过 AttemptTimeoutSeconds=1，
		// 旧行为会被 attempt 超时掐断，新行为由首字节 watchdog (5s) 兜底。
		time.Sleep(1500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"{\"result\":\"ok\"}"}}],"usage":{"total_tokens":10}}`)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	origCfg := models.AppConfig
	defer func() { models.AppConfig = origCfg }()

	models.AppConfig.AI.Native = models.NativeLLMConfig{
		Endpoints: []models.NativeEndpointConfig{{
			Name:       "default",
			BaseURL:    server.URL,
			APIKey:     "test-key",
			Model:      "glm-4-flash",
			Concurrent: 20,
		}},
		AttemptTimeoutSeconds:   1,
		FirstByteTimeoutSeconds: 5,
		IdleTimeoutSeconds:      5,
	}

	invoker := NewNativeInvoker()
	tmpOut := filepath.Join(t.TempDir(), "output.json")

	err := invoker.Invoke(AIRequest{
		PromptMsg:      "slow generation",
		OutputPath:     tmpOut,
		TimeoutMin:     1,
		ResponseFormat: "json",
	})
	if err != nil {
		t.Fatalf("expected stream to survive past AttemptTimeoutSeconds=1, got error: %v", err)
	}

	content, err := os.ReadFile(tmpOut)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(content) != `{"result":"ok"}` {
		t.Fatalf("unexpected content: %s", string(content))
	}
}
