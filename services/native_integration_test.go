package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
)

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
