package invoker

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgyInvoker_Name(t *testing.T) {
	invoker, ok := GetRawInvoker("agy")
	if !ok || invoker == nil {
		t.Fatalf("expected agy invoker to be registered, got nil")
	}
	if invoker.Name() != "agy" {
		t.Fatalf("expected invoker name 'agy', got '%s'", invoker.Name())
	}
}

func TestAgyInvoker_BuildArgs(t *testing.T) {
	tempDir := t.TempDir()
	promptFile := filepath.Join(tempDir, "analysis_prompt.md")
	if err := os.WriteFile(promptFile, []byte("# Test Antigravity Prompt"), 0644); err != nil {
		t.Fatalf("failed to write test prompt file: %v", err)
	}

	invoker := &AgyInvoker{}
	req := AIRequest{
		WorkDir:    tempDir,
		PromptFile: promptFile,
		PromptMsg:  "执行 Antigravity 静态扫描",
		InputFiles: []string{"main.go", "util.go"},
		OutputPath: filepath.Join(tempDir, "output.json.raw"),
		ModelName:  "gemini-3.5-pro",
	}

	prevOutputFormat := models.AppConfig.AI.OutputFormat
	t.Cleanup(func() { models.AppConfig.AI.OutputFormat = prevOutputFormat })

	models.AppConfig.AI.OutputFormat = "json"
	args, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "-p ") {
		t.Fatalf("expected -p flag in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions flag in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--disable-slash-commands") {
		t.Fatalf("expected --disable-slash-commands flag in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--output-format json") {
		t.Fatalf("expected --output-format json flag in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--print-timeout 60m") {
		t.Fatalf("expected --print-timeout flag in args, got %v", args)
	}
	if !strings.Contains(argsStr, "# Test Antigravity Prompt") {
		t.Fatalf("expected prompt file content in args, got %v", args)
	}
	if !strings.Contains(argsStr, "--model gemini-3.5-pro") {
		t.Fatalf("expected model flag in args, got %v", args)
	}
}

func TestAgyInvoker_InvokeWithFakeCLI(t *testing.T) {
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("failed to create fake bin dir: %v", err)
	}

	outputPath := filepath.Join(tempDir, "report.json")
	fakeAgy := filepath.Join(fakeBin, "agy")
	// 假 agy 脚本：模拟模型向指定 output path 写入合法报告
	script := `#!/bin/bash
printf '{"findings":[{"title":"fake agy finding"}]}' > ` + outputPath + `
echo "agy-finished-successfully"
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake agy: %v", err)
	}

	prevPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+prevPath)

	req := AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "执行测试扫描",
		InputFiles: []string{"main.go"},
		OutputPath: outputPath,
		TimeoutMin: 1,
	}

	invoker := &AgyInvoker{}
	if err := invoker.Invoke(req); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("expected output file at %s: %v", outputPath, err)
	}
	if !strings.Contains(string(content), "fake agy finding") {
		t.Fatalf("expected fake agy content, got: %s", string(content))
	}
}
