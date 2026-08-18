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
