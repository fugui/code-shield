package services

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeInvoker_BuildArgs(t *testing.T) {
	tempDir := t.TempDir()
	promptFile := filepath.Join(tempDir, "analysis_prompt.md")
	if err := os.WriteFile(promptFile, []byte("# OpenCode Prompt"), 0644); err != nil {
		t.Fatalf("failed to write test prompt file: %v", err)
	}

	prevFormat := models.AppConfig.AI.OutputFormat
	prevDebug := models.AppConfig.AI.DebugLogs
	t.Cleanup(func() {
		models.AppConfig.AI.OutputFormat = prevFormat
		models.AppConfig.AI.DebugLogs = prevDebug
	})
	models.AppConfig.AI.OutputFormat = "json"
	models.AppConfig.AI.DebugLogs = false

	invoker := &OpenCodeInvoker{}
	req := AIRequest{
		WorkDir:    tempDir,
		PromptFile: promptFile,
		PromptMsg:  "执行测试扫描",
		InputFiles: []string{"main.go"},
		OutputPath: filepath.Join(tempDir, "output.json.raw"),
		ModelName:  "glm5.1",
	}

	args, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	argsStr := strings.Join(args, " ")
	for _, expected := range []string{
		"run",
		"--agent shield-base-scanner",
		"--auto",
		"--format json",
		"--thinking",
		"--model glm5.1",
		"# OpenCode Prompt",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Fatalf("expected args to contain %q, got %v", expected, args)
		}
	}
}

func TestEnsureBaseAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureBaseAgent(); err != nil {
		t.Fatalf("EnsureBaseAgent failed: %v", err)
	}
	agentPath := filepath.Join(home, ".config", "opencode", "agents", BaseScannerAgentName+".md")
	content, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("failed to read base agent file: %v", err)
	}
	if string(content) != BaseScannerAgentContent {
		t.Fatalf("base agent content mismatch:\nwant %q\ngot  %q", BaseScannerAgentContent, string(content))
	}

	// 内容一致时再次调用应无错误（幂等）
	if err := EnsureBaseAgent(); err != nil {
		t.Fatalf("EnsureBaseAgent (idempotent) failed: %v", err)
	}

	// 内容被篡改后应重新写入最新内容
	if err := os.WriteFile(agentPath, []byte("stale content"), 0644); err != nil {
		t.Fatalf("failed to corrupt agent file: %v", err)
	}
	if err := EnsureBaseAgent(); err != nil {
		t.Fatalf("EnsureBaseAgent (repair) failed: %v", err)
	}
	content, _ = os.ReadFile(agentPath)
	if string(content) != BaseScannerAgentContent {
		t.Fatalf("base agent content not repaired")
	}
}

func TestCleanupLegacyTaskAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentDir := filepath.Join(home, ".config", "opencode", "agents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create agents dir: %v", err)
	}

	legacy := []string{"shield-code-review-analysis.md", "shield-memory-leak-synthesis.md"}
	kept := []string{"shield-base-scanner.md", "shield-custom.md", "other-agent.md"}
	for _, f := range append(legacy, kept...) {
		if err := os.WriteFile(filepath.Join(agentDir, f), []byte("x"), 0644); err != nil {
			t.Fatalf("failed to create fixture %s: %v", f, err)
		}
	}

	CleanupLegacyTaskAgents()

	for _, f := range legacy {
		if _, err := os.Stat(filepath.Join(agentDir, f)); !os.IsNotExist(err) {
			t.Fatalf("expected legacy agent %s to be removed, got err=%v", f, err)
		}
	}
	for _, f := range kept {
		if _, err := os.Stat(filepath.Join(agentDir, f)); err != nil {
			t.Fatalf("expected %s to be kept: %v", f, err)
		}
	}
}

func TestPrepareIsolatedDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	// 模拟已存在的 ~/.local/share/opencode/auth.json
	authDir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	authContent := `{"test_provider":{"key":"test_key_123"}}`
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(authContent), 0600); err != nil {
		t.Fatalf("failed to write auth.json: %v", err)
	}

	dataDir, cleanup, err := prepareIsolatedDataDir()
	if err != nil {
		t.Fatalf("prepareIsolatedDataDir failed: %v", err)
	}
	defer cleanup()

	// 检查目录结构
	isolatedAuthPath := filepath.Join(dataDir, "opencode", "auth.json")
	content, err := os.ReadFile(isolatedAuthPath)
	if err != nil {
		t.Fatalf("failed to read isolated auth.json: %v", err)
	}
	if string(content) != authContent {
		t.Fatalf("auth.json content mismatch: got %s, want %s", string(content), authContent)
	}

	// 验证 cleanup 后临时目录被删除
	cleanup()
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("expected dataDir to be deleted after cleanup, got err=%v", err)
	}
}
