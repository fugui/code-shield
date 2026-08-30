package services

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexInvoker_Name(t *testing.T) {
	invoker := GetAIInvoker("codex")
	if invoker == nil {
		t.Fatalf("expected codex invoker to be registered, got nil")
	}
	if invoker.Name() != "codex" {
		t.Fatalf("expected invoker name 'codex', got '%s'", invoker.Name())
	}
}

func TestCodexInvoker_BuildArgs(t *testing.T) {
	tempDir := t.TempDir()
	promptFile := filepath.Join(tempDir, "analysis_prompt.md")
	if err := os.WriteFile(promptFile, []byte("# Test Codex Prompt"), 0644); err != nil {
		t.Fatalf("failed to write test prompt file: %v", err)
	}

	invoker := &CodexInvoker{}
	req := AIRequest{
		WorkDir:    tempDir,
		PromptFile: promptFile,
		PromptMsg:  "执行测试扫描",
		InputFiles: []string{"main.go", "util.go"},
		OutputPath: filepath.Join(tempDir, "output.json.raw"),
		ModelName:  "gpt-5.6-sol",
	}

	models.AppConfig.AI.OutputFormat = "json"
	args, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "exec --skip-git-repo-check --color never") {
		t.Fatalf("expected exec flags, got %v", args)
	}
	if !strings.Contains(argsStr, "# Test Codex Prompt") {
		t.Fatalf("expected prompt file content in args, got %v", args)
	}
	if !strings.Contains(argsStr, "-m gpt-5.6-sol") {
		t.Fatalf("expected model name in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--json") {
		t.Fatalf("expected --json flag in args, got %v", args)
	}
}

func TestCodexInvoker_MockFallback(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "report.json")

	// 验证通用 mock 机制（当调用不存在的 CLI 时能够正确降级）
	req := AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "测试未安装情况下的 Mock 降级",
		OutputPath: outputPath,
		TimeoutMin: 1,
	}

	err := RunCLIProcess("non-existent-ai-cli-xyz", []string{"run"}, req, "模拟报告：AI 引擎未安装")
	if err != nil {
		t.Fatalf("expected mock fallback without error, got %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read mock output: %v", err)
	}
	if !strings.Contains(string(content), "findings") {
		t.Fatalf("expected valid JSON report in mock output, got: %s", string(content))
	}
}
