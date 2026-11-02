package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssessmentJSONSchemaModeAlwaysAuto(t *testing.T) {
	config := Config{}
	if config.AssessmentJSONSchemaMode() != "auto" {
		t.Fatalf("mode = %q, want auto", config.AssessmentJSONSchemaMode())
	}
}

func TestAppBaseDirAndTasksSeparation(t *testing.T) {
	baseDir := GetAppBaseDir()
	if baseDir == "" {
		t.Fatalf("expected non-empty baseDir")
	}

	// 确认 tasks 目录存在于基准目录下
	tasksDir := filepath.Join(baseDir, "tasks")
	if fi, err := os.Stat(tasksDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected tasks directory to exist under baseDir %s", baseDir)
	}

	cfg := Config{}
	cfg.Server.DataDir = "/var/data/custom-shield-data"

	// 1. 测试 GetTaskAbsPath: 即使 DataDir 配置为独立外部路径，tasks 依旧在 baseDir 下
	taskScript := cfg.GetTaskAbsPath("tasks/cjson-scan/precondition")
	expectedScript := filepath.Join(baseDir, "tasks/cjson-scan/precondition")
	if taskScript != expectedScript {
		t.Errorf("expected taskScript %q, got %q", expectedScript, taskScript)
	}

	// 2. 测试 GetAbsPath 对 tasks/ 的智能路由
	routedTask := cfg.GetAbsPath("tasks/memory-leak/analysis_prompt.md")
	expectedRouted := filepath.Join(baseDir, "tasks/memory-leak/analysis_prompt.md")
	if routedTask != expectedRouted {
		t.Errorf("expected routedTask %q, got %q", expectedRouted, routedTask)
	}

	// 3. 测试 GetAbsPath 对普通数据路径（如 reports/）路由到 DataDir
	routedReport := cfg.GetAbsPath("reports/code_review/2026-08-18/report-1.md")
	expectedReport := filepath.Join("/var/data/custom-shield-data", "reports/code_review/2026-08-18/report-1.md")
	if routedReport != expectedReport {
		t.Errorf("expected routedReport %q, got %q", expectedReport, routedReport)
	}
}

func TestGetDataDirCompatibility(t *testing.T) {
	t.Run("server.data_dir takes precedence", func(t *testing.T) {
		cfg := Config{}
		cfg.Server.DataDir = "/path/to/server/data"
		cfg.Storage.Root = "/path/to/old/storage"
		if got := cfg.GetDataDir(); got != "/path/to/server/data" {
			t.Errorf("expected %q, got %q", "/path/to/server/data", got)
		}
	})

	t.Run("fallback to storage.root if server.data_dir empty", func(t *testing.T) {
		cfg := Config{}
		cfg.Storage.Root = "/path/to/old/storage"
		if got := cfg.GetDataDir(); got != "/path/to/old/storage" {
			t.Errorf("expected %q, got %q", "/path/to/old/storage", got)
		}
	})

	t.Run("default fallback to ./data", func(t *testing.T) {
		cfg := Config{}
		if got := cfg.GetDataDir(); got != "./data" {
			t.Errorf("expected %q, got %q", "./data", got)
		}
	})
}

func TestLoadConfigDataDir(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 测试新配置 server.data_dir
	configFile := filepath.Join(tempDir, "config_new.yaml")
	newYAML := `
server:
  port: ":8080"
  data_dir: "` + tempDir + `/my_data"
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "test-token"
  report_base_url: "https://example.invalid"
`
	if err := os.WriteFile(configFile, []byte(newYAML), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if err := LoadConfig(configFile); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	expectedAbs, _ := filepath.Abs(tempDir + "/my_data")
	if AppConfig.Server.DataDir != expectedAbs {
		t.Errorf("expected Server.DataDir %q, got %q", expectedAbs, AppConfig.Server.DataDir)
	}
	if AppConfig.Storage.Root != expectedAbs {
		t.Errorf("expected Storage.Root %q, got %q", expectedAbs, AppConfig.Storage.Root)
	}

	// 2. 测试旧配置 storage.root 向后兼容
	configFileOld := filepath.Join(tempDir, "config_old.yaml")
	oldYAML := `
server:
  port: ":8080"
storage:
  root: "` + tempDir + `/legacy_storage"
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "test-token"
  report_base_url: "https://example.invalid"
`
	if err := os.WriteFile(configFileOld, []byte(oldYAML), 0644); err != nil {
		t.Fatalf("failed to write old config file: %v", err)
	}

	if err := LoadConfig(configFileOld); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	expectedLegacyAbs, _ := filepath.Abs(tempDir + "/legacy_storage")
	if AppConfig.Server.DataDir != expectedLegacyAbs {
		t.Errorf("expected Server.DataDir %q, got %q", expectedLegacyAbs, AppConfig.Server.DataDir)
	}
	if AppConfig.Storage.Root != expectedLegacyAbs {
		t.Errorf("expected Storage.Root %q, got %q", expectedLegacyAbs, AppConfig.Storage.Root)
	}
}

func TestLoadConfigExpandsAIFixContextToken(t *testing.T) {
	t.Setenv("AI_FIX_CONTEXT_TOKEN", "expanded-test-token")
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")
	configYAML := `
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "${AI_FIX_CONTEXT_TOKEN}"
  report_base_url: "https://example.invalid"
`
	if err := os.WriteFile(configFile, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	if err := LoadConfig(configFile); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if AppConfig.AIFix.ContextToken != "expanded-test-token" {
		t.Fatalf("expected context token to be expanded from environment")
	}
}

func TestLoadConfigNewFormat(t *testing.T) {
	configPath := filepath.Join("testdata", "config_new_format.yaml")

	if err := LoadConfig(configPath); err != nil {
		t.Fatalf("LoadConfig(%s) failed: %v", configPath, err)
	}

	// 1. 验证 LLM
	if AppConfig.LLM.DefaultResource != "native" {
		t.Errorf("expected DefaultResource 'native', got %q", AppConfig.LLM.DefaultResource)
	}
	if len(AppConfig.LLM.Resources) < 3 {
		t.Errorf("expected at least 3 resources, got %d", len(AppConfig.LLM.Resources))
	}

	// 2. 验证 Scanner
	if AppConfig.Scanner.WorkerCount < 1 {
		t.Errorf("expected WorkerCount to be derived from compute resources, got %d", AppConfig.Scanner.WorkerCount)
	}
	if !AppConfig.Scanner.Debate.Enabled {
		t.Errorf("expected Debate.Enabled to be true")
	}
	tier1Resources := AppConfig.Scanner.Debate.Tiers.Tier1Hunter.GetResources()
	if len(tier1Resources) != 2 || tier1Resources[0] != "opencode-deepseek" || tier1Resources[1] != "opencode-glm" {
		t.Errorf("expected Tier1Hunter resources ['opencode-deepseek', 'opencode-glm'], got %+v", tier1Resources)
	}
	// 3. 验证 Governance
	if AppConfig.Governance.Identity.MaxCandidatesPerObservation != 64 ||
		AppConfig.Governance.Identity.MaxCandidateEdgesPerReport != 20000 ||
		AppConfig.Governance.Identity.StrongSameThreshold != 0.90 ||
		AppConfig.Governance.Identity.AssignBand != 0.65 ||
		AppConfig.Governance.Identity.RejectBelow != 0.45 ||
		!AppConfig.GrayZoneAutoResolveEnabled() ||
		AppConfig.Governance.Identity.AIArbitrationConfidence != 0.70 ||
		AppConfig.Governance.Identity.GrayZoneFallbackMergeScore != 0.60 {
		t.Errorf("unexpected identity config: %+v", AppConfig.Governance.Identity)
	}
	if AppConfig.Governance.Arbitration.MaxCallsPerReport != 20 || AppConfig.Governance.Arbitration.TimeoutSeconds != 30 {
		t.Errorf("unexpected arbitration config: %+v", AppConfig.Governance.Arbitration)
	}
	if AppConfig.Governance.Lifecycle.ResolvedRounds != 2 ||
		AppConfig.Governance.Lifecycle.DormantThreshold != 2 ||
		AppConfig.Governance.Lifecycle.ObsoleteAfterDormant != 8 ||
		!AppConfig.Governance.Lifecycle.RequireCoverage || !AppConfig.Governance.Lifecycle.RequireChange {
		t.Errorf("unexpected lifecycle config: %+v", AppConfig.Governance.Lifecycle)
	}

	// 4. 验证 Notification
	if AppConfig.Notification.Webhook == "" {
		t.Errorf("expected non-empty Notification.Webhook")
	}

	// 5. 验证 4 阶梯的 GetTierConfig 与 GetTierResources 正常解析
	tier1 := AppConfig.GetTierConfig("tier1_hunter")
	if tier1.Backend != "opencode" || tier1.TimeoutSeconds != 10800 {
		t.Errorf("unexpected tier1 config: %+v", tier1)
	}
	tier2 := AppConfig.GetTierConfig("tier2_challenger")
	if tier2.Backend != "native" || tier2.TimeoutSeconds != 1800 {
		t.Errorf("unexpected tier2_challenger config: %+v", tier2)
	}
	tier3 := AppConfig.GetTierConfig("tier3_judge")
	if tier3.Backend != "opencode" || tier3.TimeoutSeconds != 1800 {
		t.Errorf("unexpected tier3_judge config: %+v", tier3)
	}
	tier4 := AppConfig.GetTierConfig("tier4_synthesis")
	if tier4.Backend != "native" || tier4.TimeoutSeconds != 1800 {
		t.Errorf("unexpected tier4_synthesis config: %+v", tier4)
	}
	// 6. 验证 Tier 1 多资源池切片
	tier1Res := AppConfig.GetTierResources("tier1_hunter")
	if len(tier1Res) != 2 || tier1Res[0] != "opencode-deepseek" || tier1Res[1] != "opencode-glm" {
		t.Errorf("expected tier1 resources ['opencode-deepseek', 'opencode-glm'], got %+v", tier1Res)
	}

}

func TestGetAnalysisRetryConfigDefaults(t *testing.T) {
	var cfg Config
	retry := cfg.GetAnalysisRetryConfig()

	if retry.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", retry.MaxRetries)
	}
	if retry.RetryBackoffMs != 2000 {
		t.Errorf("expected RetryBackoffMs 2000, got %d", retry.RetryBackoffMs)
	}
	if retry.MaxBackoffSeconds != 30 {
		t.Errorf("expected MaxBackoffSeconds 30, got %d", retry.MaxBackoffSeconds)
	}
	want := map[string]bool{
		"rate_limited":      true,
		"network_transient": true,
		"unknown":           true,
	}
	if len(retry.RetryableErrors) != len(want) {
		t.Fatalf("expected %d retryable classes, got %+v", len(want), retry.RetryableErrors)
	}
	for _, class := range retry.RetryableErrors {
		if !want[class] {
			t.Fatalf("unexpected retryable class %q", class)
		}
	}
}

func TestGetTierConfigUsesStageTimeoutFallback(t *testing.T) {
	cfg := Config{}
	cfg.LLM.DefaultResource = "native"
	cfg.Scanner.WorkerCount = 8
	cfg.Scanner.Debate.StageTimeoutSeconds = 1800

	tierCfg := cfg.GetTierConfig("tier3_judge")
	if tierCfg.Backend != "native" {
		t.Errorf("expected backend native, got %q", tierCfg.Backend)
	}
	if tierCfg.TimeoutSeconds != 1800 {
		t.Errorf("expected stage timeout 1800, got %d", tierCfg.TimeoutSeconds)
	}
}

func TestGetTierConfigInheritsResourceTimeoutPolicy(t *testing.T) {
	cfg := Config{}
	cfg.Scanner.Debate.StageTimeoutSeconds = 1800
	cfg.LLM.Resources = append(cfg.LLM.Resources, ComputeResourceConfig{
		ID:                      "native",
		Driver:                  "native",
		AttemptTimeoutSeconds:   300,
		FirstByteTimeoutSeconds: 90,
		IdleTimeoutSeconds:      120,
		MaxOutputBytes:          65536,
		Endpoints: []ResourceEndpointConfig{{
			Name:  "default",
			Model: "deepseek-v4-flash",
		}},
	})
	cfg.Scanner.Debate.Tiers.Tier4Synthesis.Resource = "native"

	tierCfg := cfg.GetTierConfig("tier4_synthesis")
	if tierCfg.AttemptTimeoutSeconds != 300 ||
		tierCfg.FirstByteTimeoutSeconds != 90 ||
		tierCfg.IdleTimeoutSeconds != 120 ||
		tierCfg.MaxOutputBytes != 65536 {
		t.Errorf("expected resource timeout policy to be inherited, got %+v", tierCfg)
	}
}

func TestGetTierConfigPrefersStageAttemptTimeout(t *testing.T) {
	cfg := Config{}
	cfg.LLM.Resources = append(cfg.LLM.Resources, ComputeResourceConfig{
		ID:                    "native",
		Driver:                "native",
		AttemptTimeoutSeconds: 300,
	})
	cfg.Scanner.Debate.Tiers.Tier4Synthesis.Resource = "native"
	cfg.Scanner.Debate.Tiers.Tier4Synthesis.TimeoutSeconds = 1800
	cfg.Scanner.Debate.Tiers.Tier4Synthesis.AttemptTimeoutSeconds = 900

	tierCfg := cfg.GetTierConfig("tier4_synthesis")
	if tierCfg.AttemptTimeoutSeconds != 900 {
		t.Errorf("expected stage attempt timeout 900, got %d", tierCfg.AttemptTimeoutSeconds)
	}
}

func TestScannerNormalizeDefaults(t *testing.T) {
	var scanner ScannerConfig
	scanner.NormalizeDefaults()

	if scanner.ChunkConcurrency != 6 {
		t.Errorf("expected chunk concurrency 6, got %d", scanner.ChunkConcurrency)
	}
	for tierKey, tier := range map[string]TierBindingConfig{
		"tier1_hunter":     scanner.Debate.Tiers.Tier1Hunter,
		"tier2_challenger": scanner.Debate.Tiers.Tier2Challenger,
		"tier3_judge":      scanner.Debate.Tiers.Tier3Judge,
		"tier4_synthesis":  scanner.Debate.Tiers.Tier4Synthesis,
	} {
		if tier.TimeoutSeconds <= 0 {
			t.Errorf("%s: expected stage timeout default, got %d", tierKey, tier.TimeoutSeconds)
		}
		if tier.AttemptTimeoutSeconds != 900 {
			t.Errorf("%s: expected attempt timeout 900, got %d", tierKey, tier.AttemptTimeoutSeconds)
		}
		if tier.Recovery == nil {
			t.Errorf("%s: expected default recovery config", tierKey)
			continue
		}
		if tier.Recovery.MaxTotalAttempts != 2 ||
			tier.Recovery.MaxAttemptsPerResource != 2 ||
			tier.Recovery.MaxCandidateFailovers != 0 {
			t.Errorf("%s: recovery defaults = %+v, want same-resource retry with no failover", tierKey, tier.Recovery)
		}
	}
	if scanner.Debate.Tiers.Tier1Hunter.IdleTimeoutSeconds != 600 {
		t.Errorf("expected Tier 1 idle timeout 600, got %d", scanner.Debate.Tiers.Tier1Hunter.IdleTimeoutSeconds)
	}
	for _, tier := range []TierBindingConfig{
		scanner.Debate.Tiers.Tier2Challenger,
		scanner.Debate.Tiers.Tier4Synthesis,
	} {
		if tier.FirstByteTimeoutSeconds != 180 {
			t.Errorf("expected first-byte timeout 180, got %d", tier.FirstByteTimeoutSeconds)
		}
		if tier.IdleTimeoutSeconds != 300 {
			t.Errorf("expected stream idle timeout 300, got %d", tier.IdleTimeoutSeconds)
		}
	}
}

func TestCategoryRepairResourceDefaultsToSchemaRepairResource(t *testing.T) {
	var scanner ScannerConfig
	scanner.Artifact.SchemaRepairResource = "repair-native"
	scanner.NormalizeDefaults()
	if scanner.Artifact.CategoryRepairResource != "repair-native" {
		t.Fatalf("category repair resource = %q, want repair-native", scanner.Artifact.CategoryRepairResource)
	}
}

func TestValidateTierBindingsRejectsDisallowedEngine(t *testing.T) {
	cfg := Config{}
	cfg.LLM.Resources = append(cfg.LLM.Resources, ComputeResourceConfig{
		ID:     "native",
		Driver: "native",
	})
	cfg.Scanner.Debate.Tiers.Tier1Hunter.Resource = "native"

	err := ValidateTierBindings(&cfg.LLM, &cfg.Scanner)
	if err == nil {
		t.Fatal("expected Tier 1 native binding to be rejected")
	}
}

func TestParseAssessmentConfig(t *testing.T) {
	envelope, err := ParseAssessmentConfig([]byte(`{
		"version":1,
		"profile":"occurrencereview",
		"schema":"code-shield.assessment-config.v1",
		"domain":"thread_creation"
	}`))
	if err != nil {
		t.Fatalf("ParseAssessmentConfig() error = %v", err)
	}
	if envelope.Profile != "occurrencereview" || envelope.Domain != "thread_creation" {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if _, err := ParseAssessmentConfig([]byte(`{"version":1,"profile":"","schema":"code-shield.assessment-config.v1","domain":"d"}`)); err == nil {
		t.Fatal("ParseAssessmentConfig() missing profile = nil, want error")
	}
}

func TestComputeResourceConcurrentMarshalRoundTrip(t *testing.T) {
	recordJSON := `{
		"default_resource": "native",
		"debug_logs": false,
		"resources": [
			{"id": "opencode-deepseek", "driver": "opencode", "concurrent": 40},
			{"id": "native", "driver": "native", "endpoints": [{"name": "h100", "base_url": "http://10.32.4.9:30000/v1", "model": "dp", "concurrent": 20}]}
		]
	}`
	var llm LLMConfig
	if err := json.Unmarshal([]byte(recordJSON), &llm); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if got := llm.Resources[0].ResourceConcurrent(); got != 40 {
		t.Fatalf("opencode ResourceConcurrent = %d, want 40", got)
	}
	if got := llm.Resources[1].ResourceConcurrent(); got != 20 {
		t.Fatalf("native ResourceConcurrent = %d, want 20", got)
	}

	// /api/admin/config/full 序列化时不得丢失并发槽位字段。
	out, err := json.Marshal(llm)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var round LLMConfig
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("second Unmarshal failed: %v", err)
	}
	if got := round.Resources[0].ResourceConcurrent(); got != 40 {
		t.Fatalf("round-trip opencode ResourceConcurrent = %d, want 40 (保存后并发丢失?)", got)
	}
	if got := round.Resources[1].ResourceConcurrent(); got != 20 {
		t.Fatalf("round-trip native ResourceConcurrent = %d, want 20", got)
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(out, &asMap); err != nil {
		t.Fatalf("json parse failed: %v", err)
	}
	res, ok := asMap["resources"].([]interface{})
	if !ok || len(res) != 2 {
		t.Fatalf("unexpected resources shape: %s", out)
	}
	first, ok := res[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected resource shape: %s", out)
	}
	conc, ok := first["concurrent"]
	if !ok {
		t.Fatalf("serialized resource missing top-level concurrent: %s", out)
	}
	if v, _ := conc.(float64); int(v) != 40 {
		t.Fatalf("serialized concurrent = %v, want 40", conc)
	}
}

func TestComputeResourceModelMarshalRoundTrip(t *testing.T) {
	recordJSON := `{
		"default_resource": "native",
		"debug_logs": false,
		"resources": [
			{"id": "opencode-deepseek", "driver": "opencode", "model": "deepseek-v4"},
			{"id": "native", "driver": "native", "base_url": "http://10.32.4.9:30000/v1", "model": "glm-4-flash", "concurrent": 20}
		]
	}`
	var llm LLMConfig
	if err := json.Unmarshal([]byte(recordJSON), &llm); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	out, err := json.Marshal(llm)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var round LLMConfig
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("second Unmarshal failed: %v", err)
	}
	if got := round.Resources[0].ResourceModel(); got != "deepseek-v4" {
		t.Fatalf("round-trip opencode ResourceModel = %q, want %q", got, "deepseek-v4")
	}
	if got := round.Resources[1].ResourceModel(); got != "glm-4-flash" {
		t.Fatalf("round-trip native ResourceModel = %q, want %q", got, "glm-4-flash")
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(out, &asMap); err != nil {
		t.Fatalf("json parse failed: %v", err)
	}
	res, ok := asMap["resources"].([]interface{})
	if !ok || len(res) != 2 {
		t.Fatalf("unexpected resources shape: %s", out)
	}
	for _, resource := range res {
		item, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource shape: %s", out)
		}
		if _, ok := item["model"]; !ok {
			t.Fatalf("serialized resource missing top-level model: %s", out)
		}
	}
}

func TestLoadConfig_ScannerWorkerCountDeterminism(t *testing.T) {
	aiFixStub := `
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "test-token"
  report_base_url: "https://example.invalid"
`

	// 场景 1: config.yaml 显式配置 scanner.worker_count: 5，即使 LLM 总并发为 80，加载后必须坚决为 5，严禁被推导为 40
	yamlWithExplicitWorker := `
llm:
  default_resource: "native"
  resources:
    - id: "native-1"
      driver: "native"
      concurrent: 40
    - id: "native-2"
      driver: "native"
      concurrent: 40
scanner:
  worker_count: 5
` + aiFixStub
	tmpFile1 := filepath.Join(t.TempDir(), "config_explicit.yaml")
	if err := os.WriteFile(tmpFile1, []byte(yamlWithExplicitWorker), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := LoadConfig(tmpFile1); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if AppConfig.Scanner.WorkerCount != 5 {
		t.Fatalf("expected Scanner.WorkerCount == 5, got %d (explicit configuration was overwritten!)", AppConfig.Scanner.WorkerCount)
	}

	// 场景 2: 未配置 scanner.worker_count (缺省为 0)，LLM 资源并发之和为 80，动态推导 (80+1)/2 = 40
	yamlWithDefaultWorker := `
llm:
  default_resource: "native"
  resources:
    - id: "native-1"
      driver: "native"
      concurrent: 40
    - id: "native-2"
      driver: "native"
      concurrent: 40
scanner:
  output_format: "text"
` + aiFixStub
	tmpFile2 := filepath.Join(t.TempDir(), "config_derived.yaml")
	if err := os.WriteFile(tmpFile2, []byte(yamlWithDefaultWorker), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := LoadConfig(tmpFile2); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if AppConfig.Scanner.WorkerCount != 40 {
		t.Fatalf("expected Scanner.WorkerCount derived to 40, got %d", AppConfig.Scanner.WorkerCount)
	}

	// 场景 3: 未配置 scanner.worker_count 且无 LLM 节点，兜底为 5
	yamlEmptyLLM := `
scanner:
  output_format: "text"
` + aiFixStub
	tmpFile3 := filepath.Join(t.TempDir(), "config_empty.yaml")
	if err := os.WriteFile(tmpFile3, []byte(yamlEmptyLLM), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := LoadConfig(tmpFile3); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if AppConfig.Scanner.WorkerCount != 5 {
		t.Fatalf("expected fallback Scanner.WorkerCount == 5, got %d", AppConfig.Scanner.WorkerCount)
	}
	if AppConfig.Scanner.MaxQueueSize != 2000 {
		t.Fatalf("expected fallback Scanner.MaxQueueSize == 2000, got %d", AppConfig.Scanner.MaxQueueSize)
	}
}

func TestLoadConfig_GovernanceDirectNormalization(t *testing.T) {
	yamlContent := `
governance:
  identity:
    algorithm_version: "v2-custom"
    max_candidates_per_observation: 128
    strong_same_threshold: 0.95
    assign_band: 0.70
  arbitration:
    context_lines: 15
    timeout_seconds: 45
  lifecycle:
    resolved_rounds: 5
    dormant_rounds: 3
scanner:
  worker_count: 3
ai_fix:
  fix_url: "https://example.invalid/fixdefect?id={defect_id}"
  context_token: "test-token"
  report_base_url: "https://example.invalid"
`
	tmpFile := filepath.Join(t.TempDir(), "config_governance.yaml")
	if err := os.WriteFile(tmpFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := LoadConfig(tmpFile); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// 验证嵌套参数原样保留，没有被默认顶层空字段覆写
	if AppConfig.Governance.Identity.AlgorithmVersion != "v2-custom" {
		t.Errorf("expected algorithm_version 'v2-custom', got %q", AppConfig.Governance.Identity.AlgorithmVersion)
	}
	if AppConfig.Governance.Identity.MaxCandidatesPerObservation != 128 {
		t.Errorf("expected max_candidates_per_observation 128, got %d", AppConfig.Governance.Identity.MaxCandidatesPerObservation)
	}
	if AppConfig.Governance.Identity.StrongSameThreshold != 0.95 {
		t.Errorf("expected strong_same_threshold 0.95, got %f", AppConfig.Governance.Identity.StrongSameThreshold)
	}
	if AppConfig.Governance.Identity.AssignBand != 0.70 {
		t.Errorf("expected assign_band 0.70, got %f", AppConfig.Governance.Identity.AssignBand)
	}
	if AppConfig.Governance.Arbitration.ContextLines != 15 {
		t.Errorf("expected context_lines 15, got %d", AppConfig.Governance.Arbitration.ContextLines)
	}
	if AppConfig.Governance.Arbitration.TimeoutSeconds != 45 {
		t.Errorf("expected timeout_seconds 45, got %d", AppConfig.Governance.Arbitration.TimeoutSeconds)
	}
	if AppConfig.Governance.Lifecycle.ResolvedRounds != 5 {
		t.Errorf("expected resolved_rounds 5, got %d", AppConfig.Governance.Lifecycle.ResolvedRounds)
	}
	if AppConfig.Governance.Lifecycle.DormantThreshold != 3 {
		t.Errorf("expected dormant_rounds 3, got %d", AppConfig.Governance.Lifecycle.DormantThreshold)
	}

	// 验证未显式配置的子字段被 NormalizeDefaults 补齐
	if AppConfig.Governance.Identity.MaxCandidateEdgesPerReport != 20000 {
		t.Errorf("expected max_candidate_edges_per_report default 20000, got %d", AppConfig.Governance.Identity.MaxCandidateEdgesPerReport)
	}
	if AppConfig.Governance.Lifecycle.ObsoleteAfterDormant != 8 {
		t.Errorf("expected obsolete_after_dormant default 8, got %d", AppConfig.Governance.Lifecycle.ObsoleteAfterDormant)
	}
}

func TestDynamicComputeAtomicSnapshotConcurrency(t *testing.T) {
	initial := DynamicComputeConfig{
		LLM: LLMConfig{
			DefaultResource: "native-1",
			Resources: []ComputeResourceConfig{
				{
					ID:         "native-1",
					Driver:     "native",
					Concurrent: 10,
					Endpoints: []ResourceEndpointConfig{
						{Name: "ep-1", BaseURL: "http://localhost:8000", Concurrent: 10},
					},
				},
				{
					ID:         "agy-1",
					Driver:     "agy",
					Concurrent: 5,
				},
			},
		},
		Tiers: DebateTiersConfig{
			Tier1Hunter: TierBindingConfig{
				Resource:       "agy-1",
				TimeoutSeconds: 1800,
			},
			Tier2Challenger: TierBindingConfig{
				Resource:       "native-1",
				TimeoutSeconds: 1200,
			},
		},
		WorkerCount: 5,
	}
	ApplyDynamicComputeConfig(initial)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 启动多个并发读取协程
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = GetComputeConfig()
					_ = AppConfig.FindResource("native-1")
					_ = AppConfig.GetTierConfig("tier1_hunter")
					_ = AppConfig.GetTierResources("tier2_challenger")
					_ = GetWorkerCount()
				}
			}
		}()
	}

	// 启动并发写入协程
	for i := 0; i < 4; i++ {
		wg.Add(1)
		workerID := i
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				next := DynamicComputeConfig{
					LLM: LLMConfig{
						DefaultResource: "native-1",
						Resources: []ComputeResourceConfig{
							{
								ID:         "native-1",
								Driver:     "native",
								Concurrent: 10 + j,
								Endpoints: []ResourceEndpointConfig{
									{Name: "ep-1", BaseURL: "http://localhost:8000", Concurrent: 10 + j},
								},
							},
							{
								ID:         "agy-1",
								Driver:     "agy",
								Concurrent: 5 + workerID,
							},
						},
					},
					Tiers: DebateTiersConfig{
						Tier1Hunter: TierBindingConfig{
							Resources:      []string{"agy-1"},
							TimeoutSeconds: 1800 + j,
						},
					},
					WorkerCount: 5 + j%5,
				}
				ApplyDynamicComputeConfig(next)
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestToolResource_FallbackDanglingResource(t *testing.T) {
	cfg := Config{}
	cfg.LLM.DefaultResource = "native"
	cfg.Scanner.Tools.Overrides = map[string]string{
		"repair_json": "deleted-resource-id",
	}

	// 算力快照中只有 native 和 agy
	comp := DynamicComputeConfig{
		LLM: LLMConfig{
			DefaultResource: "native",
			Resources: []ComputeResourceConfig{
				{ID: "native", Driver: "native"},
				{ID: "agy", Driver: "agy"},
			},
		},
	}
	ApplyDynamicComputeConfig(comp)

	// repair_json 指定的 deleted-resource-id 悬空，必须平滑回退至 default_resource ("native")
	resolved := cfg.ToolResource("repair_json")
	if resolved != "native" {
		t.Fatalf("expected dangling resource to fallback to 'native', got %q", resolved)
	}

	// 若指定的资源为有效内置驱动名称（如 "opencode"），无需在 Resources 中显式定义也允许透传
	cfg.Scanner.Tools.Overrides["repair_json"] = "opencode"
	if res := cfg.ToolResource("repair_json"); res != "opencode" {
		t.Fatalf("expected builtin driver 'opencode' to be retained, got %q", res)
	}
}

func TestValidateComputeConfig(t *testing.T) {
	valid := &DynamicComputeConfig{
		LLM: LLMConfig{
			DefaultResource: "native-1",
			Resources: []ComputeResourceConfig{
				{
					ID:     "native-1",
					Driver: "native",
					Endpoints: []ResourceEndpointConfig{
						{Name: "default", BaseURL: "http://localhost:8000/chat/completions"},
					},
				},
				{
					ID:     "agy-1",
					Driver: "agy",
				},
			},
		},
		Tiers: DebateTiersConfig{
			Tier1Hunter:     TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800, AttemptTimeoutSeconds: 900},
			Tier2Challenger: TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200, AttemptTimeoutSeconds: 600},
			Tier3Judge:      TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1800, AttemptTimeoutSeconds: 900},
			Tier4Synthesis:  TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1200, AttemptTimeoutSeconds: 600},
		},
		WorkerCount: 5,
	}

	// 1. 合法配置校验通过
	if err := ValidateComputeConfig(valid, nil); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}

	// 2. 违反引擎类型强约束：Tier 1 (Hunter) 绑定了 Native
	invalidTier1 := *valid
	invalidTier1.Tiers.Tier1Hunter = TierBindingConfig{Resource: "native-1", TimeoutSeconds: 1800}
	if err := ValidateComputeConfig(&invalidTier1, nil); err == nil {
		t.Fatal("expected error when Tier 1 bound to native, got nil")
	}

	// 3. 违反引擎类型强约束：Tier 2 (Challenger) 绑定了 Thick Agent
	invalidTier2 := *valid
	invalidTier2.Tiers.Tier2Challenger = TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 1200}
	if err := ValidateComputeConfig(&invalidTier2, nil); err == nil {
		t.Fatal("expected error when Tier 2 bound to agy, got nil")
	}

	// 4. 超时倒挂：单次超时大于阶段预算
	invalidTimeout := *valid
	invalidTimeout.Tiers.Tier1Hunter = TierBindingConfig{Resource: "agy-1", TimeoutSeconds: 600, AttemptTimeoutSeconds: 900}
	if err := ValidateComputeConfig(&invalidTimeout, nil); err == nil {
		t.Fatal("expected error when attempt_timeout > timeout, got nil")
	}

	// 5. Native 节点缺少 Endpoints
	invalidNative := *valid
	invalidNative.LLM.Resources = []ComputeResourceConfig{
		{ID: "native-1", Driver: "native", Endpoints: nil},
		{ID: "agy-1", Driver: "agy"},
	}
	if err := ValidateComputeConfig(&invalidNative, nil); err == nil {
		t.Fatal("expected error when native node has no endpoints, got nil")
	}
}

func TestMigrateLegacyDynamicConfigsOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test_migrate.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SystemDynamicConfig{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	// 注入旧版 llm 与 scanner 记录，以及旧版 governance 和 notification
	legacyLLM := LLMConfig{
		DefaultResource: "legacy-native",
		Resources: []ComputeResourceConfig{
			{ID: "legacy-native", Driver: "native", Endpoints: []ResourceEndpointConfig{{Name: "ep", BaseURL: "http://127.0.0.1:8000"}}},
			{ID: "legacy-agy", Driver: "agy"},
		},
	}
	llmJSON, _ := json.Marshal(legacyLLM)

	var legacyScanner struct {
		WorkerCount int `json:"worker_count"`
		Debate      struct {
			Tiers DebateTiersConfig `json:"tiers"`
		} `json:"debate"`
	}
	legacyScanner.WorkerCount = 12
	legacyScanner.Debate.Tiers.Tier1Hunter = TierBindingConfig{Resource: "legacy-agy", TimeoutSeconds: 3600}
	legacyScanner.Debate.Tiers.Tier2Challenger = TierBindingConfig{Resource: "legacy-native", TimeoutSeconds: 1500}
	scannerJSON, _ := json.Marshal(legacyScanner)

	now := time.Now()
	_ = db.Create(&SystemDynamicConfig{Category: "llm", Data: datatypes.JSON(llmJSON), Version: 3, CreatedAt: now, UpdatedAt: now})
	_ = db.Create(&SystemDynamicConfig{Category: "scanner", Data: datatypes.JSON(scannerJSON), Version: 5, CreatedAt: now, UpdatedAt: now})
	_ = db.Create(&SystemDynamicConfig{Category: "governance", Data: datatypes.JSON([]byte(`{"identity":{}}`)), Version: 2, CreatedAt: now, UpdatedAt: now})
	_ = db.Create(&SystemDynamicConfig{Category: "notification", Data: datatypes.JSON([]byte(`{"webhook":"http://example.com"}`)), Version: 1, CreatedAt: now, UpdatedAt: now})

	// 执行迁移
	if err := migrateLegacyDynamicConfigsOnce(db); err != nil {
		t.Fatalf("migrateLegacyDynamicConfigsOnce failed: %v", err)
	}

	// 验证生成了唯一的 category = 'compute'
	var computeRecord SystemDynamicConfig
	if err := db.Where("category = ?", "compute").First(&computeRecord).Error; err != nil {
		t.Fatalf("expected category 'compute' record created: %v", err)
	}
	if computeRecord.Version != 1 || computeRecord.UpdatedBy != "system_migration" {
		t.Fatalf("unexpected version or updated_by: version=%d, updated_by=%s", computeRecord.Version, computeRecord.UpdatedBy)
	}

	var migrated DynamicComputeConfig
	if err := json.Unmarshal(computeRecord.Data, &migrated); err != nil {
		t.Fatalf("unmarshal migrated compute data: %v", err)
	}
	if migrated.WorkerCount != 12 {
		t.Errorf("expected WorkerCount 12, got %d", migrated.WorkerCount)
	}
	if migrated.LLM.DefaultResource != "legacy-native" {
		t.Errorf("expected DefaultResource 'legacy-native', got %q", migrated.LLM.DefaultResource)
	}
	if migrated.Tiers.Tier1Hunter.Resource != "legacy-agy" || migrated.Tiers.Tier1Hunter.TimeoutSeconds != 3600 {
		t.Errorf("unexpected Tier1Hunter: %+v", migrated.Tiers.Tier1Hunter)
	}

	// 验证旧版 4 类配置记录已被彻底清除
	var remainingCount int64
	db.Model(&SystemDynamicConfig{}).Where("category IN ('llm', 'scanner', 'governance', 'notification')").Count(&remainingCount)
	if remainingCount != 0 {
		t.Fatalf("expected 0 legacy records remaining, got %d", remainingCount)
	}

	// 幂等性测试：再次执行不会覆盖已有 compute
	migrated.WorkerCount = 18
	newJSON, _ := json.Marshal(migrated)
	db.Model(&SystemDynamicConfig{}).Where("category = ?", "compute").Updates(map[string]interface{}{
		"data":    datatypes.JSON(newJSON),
		"version": 2,
	})
	if err := migrateLegacyDynamicConfigsOnce(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	var afterRecord SystemDynamicConfig
	db.Where("category = ?", "compute").First(&afterRecord)
	if afterRecord.Version != 2 {
		t.Fatalf("expected version 2 preserved, got %d", afterRecord.Version)
	}
}

func TestConfigYamlExampleParsesCleanly(t *testing.T) {
	examplePath := filepath.Join("..", "config.yaml.example")
	cfg, err := ParseConfigFile(examplePath)
	if err != nil {
		t.Fatalf("ParseConfigFile(config.yaml.example) failed: %v", err)
	}

	// 验证冷配置加载
	if cfg.Governance.Identity.AlgorithmVersion != "v2" {
		t.Errorf("expected Governance.Identity.AlgorithmVersion 'v2', got %q", cfg.Governance.Identity.AlgorithmVersion)
	}
	if cfg.Governance.Lifecycle.ResolvedRounds != 3 {
		t.Errorf("expected Governance.Lifecycle.ResolvedRounds 3, got %d", cfg.Governance.Lifecycle.ResolvedRounds)
	}

	// 验证热配置种子可正常推导并符合强约束校验
	seed := DynamicComputeConfig{
		LLM:         cfg.LLM,
		Tiers:       cfg.Scanner.Debate.Tiers,
		WorkerCount: cfg.Scanner.WorkerCount,
	}
	if seed.WorkerCount != 5 {
		t.Errorf("expected WorkerCount 5, got %d", seed.WorkerCount)
	}
	if err := ValidateComputeConfig(&seed, &cfg); err != nil {
		t.Fatalf("ValidateComputeConfig failed for seed: %v", err)
	}
}
