package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAssessmentJSONSchemaModeDefaultsOff(t *testing.T) {
	config := Config{}
	if config.AssessmentJSONSchemaMode() != "off" {
		t.Fatalf("mode = %q, want off", config.AssessmentJSONSchemaMode())
	}
	config.Scanner.Artifact.AssessmentJSONSchemaMode = "AUTO"
	if config.AssessmentJSONSchemaMode() != "auto" {
		t.Fatalf("mode = %q, want auto", config.AssessmentJSONSchemaMode())
	}
	config.Scanner.Artifact.AssessmentJSONSchemaMode = "unsupported"
	if config.AssessmentJSONSchemaMode() != "off" {
		t.Fatalf("mode = %q, want off", config.AssessmentJSONSchemaMode())
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
	if AppConfig.Identity.MaxCandidatesPerObservation != 64 ||
		AppConfig.Identity.MaxCandidateEdgesPerReport != 20000 ||
		AppConfig.Identity.StrongSameThreshold != 0.90 ||
		AppConfig.Identity.AssignBand != 0.65 ||
		AppConfig.Identity.RejectBelow != 0.45 ||
		!AppConfig.GrayZoneAutoResolveEnabled() ||
		AppConfig.Identity.AIArbitrationConfidence != 0.70 ||
		AppConfig.Identity.GrayZoneFallbackMergeScore != 0.60 {
		t.Errorf("unexpected identity config: %+v", AppConfig.Identity)
	}
	if AppConfig.Arbitration.MaxCallsPerReport != 20 || AppConfig.Arbitration.TimeoutSeconds != 30 {
		t.Errorf("unexpected arbitration config: %+v", AppConfig.Arbitration)
	}
	if AppConfig.Lifecycle.ResolvedRounds != 2 ||
		AppConfig.Lifecycle.DormantThreshold != 2 ||
		AppConfig.Lifecycle.ObsoleteAfterDormant != 8 ||
		!AppConfig.Lifecycle.RequireCoverage || !AppConfig.Lifecycle.RequireChange {
		t.Errorf("unexpected lifecycle config: %+v", AppConfig.Lifecycle)
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

func TestNormalizeNativeEndpointsLegacyShorthand(t *testing.T) {
	configYAML := `
llm:
  resources:
    - id: "native"
      driver: "native"
      base_url: "http://legacy.invalid/v1/chat/completions"
      api_key: "legacy-key"
      model: "legacy-model"
      concurrent: 7
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(configYAML), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	cfg.SyncLegacy()

	resource := cfg.FindResource("native")
	if resource == nil {
		t.Fatal("expected native resource")
	}
	if len(resource.Endpoints) != 1 {
		t.Fatalf("expected one endpoint, got %+v", resource.Endpoints)
	}
	endpoint := resource.Endpoints[0]
	if endpoint.Name != "default" ||
		endpoint.BaseURL != "http://legacy.invalid/v1/chat/completions" ||
		endpoint.APIKey != "legacy-key" ||
		endpoint.Model != "legacy-model" ||
		endpoint.Concurrent != 7 {
		t.Fatalf("unexpected normalized endpoint: %+v", endpoint)
	}
}

func TestNormalizeNativeEndpointsFromLegacyAINative(t *testing.T) {
	configYAML := `
ai:
  native:
    base_url: "http://legacy.invalid/v1/chat/completions"
    api_key: "legacy-key"
    default_model: "legacy-model"
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(configYAML), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	cfg.SyncLegacy()

	resource := cfg.FindResource("native")
	if resource == nil || len(resource.Endpoints) != 1 {
		t.Fatalf("expected normalized native endpoint, got %+v", resource)
	}
	endpoint := resource.Endpoints[0]
	if endpoint.BaseURL != "http://legacy.invalid/v1/chat/completions" ||
		endpoint.APIKey != "legacy-key" ||
		endpoint.Model != "legacy-model" ||
		endpoint.Concurrent != 20 {
		t.Fatalf("unexpected legacy endpoint: %+v", endpoint)
	}
}

func TestNormalizeNativeEndpointsFromLegacyDatabaseJSON(t *testing.T) {
	recordJSON := `{
		"default_resource": "native",
		"resources": [{
			"id": "native",
			"driver": "native",
			"base_url": "http://database.invalid/v1/chat/completions",
			"api_key": "database-key",
			"model": "database-model",
			"concurrent": 9
		}]
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(recordJSON), &cfg.LLM); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	cfg.SyncLegacy()

	resource := cfg.FindResource("native")
	if resource == nil || len(resource.Endpoints) != 1 {
		t.Fatalf("expected normalized native endpoint, got %+v", resource)
	}
	endpoint := resource.Endpoints[0]
	if endpoint.BaseURL != "http://database.invalid/v1/chat/completions" ||
		endpoint.APIKey != "database-key" ||
		endpoint.Model != "database-model" ||
		endpoint.Concurrent != 9 {
		t.Fatalf("unexpected database endpoint: %+v", endpoint)
	}
}

func TestGetTierConfigUsesStageTimeoutFallback(t *testing.T) {
	cfg := Config{}
	cfg.AI.Backend = "native"
	cfg.Server.WorkerCount = 8
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
