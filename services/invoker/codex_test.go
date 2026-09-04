package invoker

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexInvoker_Name(t *testing.T) {
	invoker, ok := GetRawInvoker("codex")
	if !ok || invoker == nil {
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

	// 保存并恢复全局配置，避免测试间状态污染
	prevOutputFormat := models.AppConfig.AI.OutputFormat
	t.Cleanup(func() { models.AppConfig.AI.OutputFormat = prevOutputFormat })

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
	if !strings.Contains(argsStr, "--output-last-message") {
		t.Fatalf("expected --output-last-message flag in args, got %v", args)
	}
	if strings.Contains(argsStr, "--json") {
		t.Fatalf("expected no --json flag (it emits JSONL events, not model output), got %v", args)
	}
	if !strings.Contains(argsStr, "output.json.raw.lastmsg") {
		t.Fatalf("expected last-message capture path derived from OutputPath, got %v", args)
	}
}

func TestFinalizeCodexOutput(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 模型已写盘 → 保留模型产物并清理捕获文件
	modelWritten := filepath.Join(tempDir, "model-written.json")
	if err := os.WriteFile(modelWritten, []byte(`{"source":"model"}`), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	if err := os.WriteFile(modelWritten+".lastmsg", []byte(`{"source":"lastmsg"}`), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	if err := finalizeCodexOutput(AIRequest{OutputPath: modelWritten}); err != nil {
		t.Fatalf("finalize (model written) failed: %v", err)
	}
	content, _ := os.ReadFile(modelWritten)
	if string(content) != `{"source":"model"}` {
		t.Fatalf("model-written output should be preserved, got %s", string(content))
	}
	if _, statErr := os.Stat(modelWritten + ".lastmsg"); !os.IsNotExist(statErr) {
		t.Fatalf("lastmsg should be cleaned up, stat err=%v", statErr)
	}

	// 2. 模型未写盘 → 回退使用 lastmsg
	fallback := filepath.Join(tempDir, "fallback.json")
	if err := os.WriteFile(fallback+".lastmsg", []byte(`{"source":"lastmsg"}`), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	if err := finalizeCodexOutput(AIRequest{OutputPath: fallback}); err != nil {
		t.Fatalf("finalize (fallback) failed: %v", err)
	}
	content, _ = os.ReadFile(fallback)
	if string(content) != `{"source":"lastmsg"}` {
		t.Fatalf("expected lastmsg fallback content, got %s", string(content))
	}

	// 3. 两者都不存在 → 返回错误
	missing := filepath.Join(tempDir, "missing.json")
	if err := finalizeCodexOutput(AIRequest{OutputPath: missing}); err == nil {
		t.Fatalf("expected error when no output available")
	}
}

func TestCodexInvoker_InvokeCapturesLastMessage(t *testing.T) {
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("failed to create fake bin dir: %v", err)
	}
	fakeCodex := filepath.Join(fakeBin, "codex")
	script := `#!/bin/bash
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift 2; continue; fi
  shift
done
printf '{"findings":[{"title":"fake"}]}' > "$out"
echo "codex-run-ok"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake codex: %v", err)
	}

	prevPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+prevPath)

	outputPath := filepath.Join(tempDir, "report.json.raw")
	req := AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "执行测试扫描",
		InputFiles: []string{"main.go"},
		OutputPath: outputPath,
		TimeoutMin: 1,
	}

	invoker := &CodexInvoker{}
	if err := invoker.Invoke(req); err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("expected captured output at %s: %v", outputPath, err)
	}
	if !strings.Contains(string(content), "fake") {
		t.Fatalf("expected last-message content in output, got: %s", string(content))
	}
	if _, statErr := os.Stat(outputPath + ".lastmsg"); !os.IsNotExist(statErr) {
		t.Fatalf("expected lastmsg capture file to be cleaned, stat err=%v", statErr)
	}
}
