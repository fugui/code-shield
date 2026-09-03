package models

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestLoadConfigNewFormat(t *testing.T) {
	// 测试直接加载实际根目录的 config.yaml
	repoRoot := GetAppBaseDir()
	configPath := filepath.Join(repoRoot, "config.yaml")

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
	if AppConfig.Scanner.WorkerCount != 5 {
		t.Errorf("expected WorkerCount 5, got %d", AppConfig.Scanner.WorkerCount)
	}
	if !AppConfig.Scanner.Debate.Enabled {
		t.Errorf("expected Debate.Enabled to be true")
	}
	if AppConfig.Scanner.Debate.Tiers.Tier1Hunter.Resource != "agy" {
		t.Errorf("expected Tier1Hunter resource 'agy', got %q", AppConfig.Scanner.Debate.Tiers.Tier1Hunter.Resource)
	}
	if AppConfig.Scanner.Debate.Tiers.Tier2Reasoning.Resource != "agy" {
		t.Errorf("expected Tier2Reasoning resource 'agy', got %q", AppConfig.Scanner.Debate.Tiers.Tier2Reasoning.Resource)
	}
	if AppConfig.Scanner.Debate.Tiers.Tier3Synthesis.Resource != "native" {
		t.Errorf("expected Tier3Synthesis resource 'native', got %q", AppConfig.Scanner.Debate.Tiers.Tier3Synthesis.Resource)
	}

	// 3. 验证 Governance
	if !AppConfig.Governance.Fingerprint.Enabled {
		t.Errorf("expected Fingerprint.Enabled to be true")
	}
	if AppConfig.Governance.Fingerprint.SimilarityThreshold != 0.85 {
		t.Errorf("expected SimilarityThreshold 0.85, got %f", AppConfig.Governance.Fingerprint.SimilarityThreshold)
	}

	// 4. 验证 Notification
	if AppConfig.Notification.Webhook == "" {
		t.Errorf("expected non-empty Notification.Webhook")
	}

	// 5. 验证 GetTierConfig 正常从新格式解析
	tier1 := AppConfig.GetTierConfig("tier1_fast")
	if tier1.Backend != "agy" || tier1.TimeoutSeconds != 1200 {
		t.Errorf("unexpected tier1 config: %+v", tier1)
	}
	tier3 := AppConfig.GetTierConfig("tier3_synthesis")
	if tier3.Backend != "native" || tier3.TimeoutSeconds != 300 {
		t.Errorf("unexpected tier3 config: %+v", tier3)
	}
}
