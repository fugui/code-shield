package services

import (
	"code-shield/models"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildPromptPayload(t *testing.T) {
	tempDir := t.TempDir()
	promptFile := filepath.Join(tempDir, "analysis_prompt.md")
	if err := os.WriteFile(promptFile, []byte("# 规则内容"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	req := AIRequest{
		PromptFile: promptFile,
		PromptMsg:  "执行扫描",
		InputFiles: []string{"a.go", "b.go"},
		OutputPath: filepath.Join(tempDir, "out.json.raw"),
	}

	// includePromptFile=true 时内联提示词文件
	inlined, err := BuildPromptPayload(req, true)
	if err != nil {
		t.Fatalf("BuildPromptPayload(inline=true) failed: %v", err)
	}
	if !strings.Contains(inlined, "# 规则内容") {
		t.Fatalf("expected prompt file content inlined, got %q", inlined)
	}
	if !strings.Contains(inlined, "任务采用分片执行，本次只") {
		t.Fatalf("expected chunked marker for .json.raw with multiple inputs, got %q", inlined)
	}

	// includePromptFile=false 时不内联提示词文件
	notInlined, err := BuildPromptPayload(req, false)
	if err != nil {
		t.Fatalf("BuildPromptPayload(inline=false) failed: %v", err)
	}
	if strings.Contains(notInlined, "# 规则内容") {
		t.Fatalf("prompt file content should not be inlined, got %q", notInlined)
	}

	// 提示词文件缺失时应返回错误
	req.PromptFile = filepath.Join(tempDir, "missing.md")
	if _, err := BuildPromptPayload(req, true); err == nil {
		t.Fatalf("expected error for missing prompt file")
	}
}

func TestRunCLIProcess_MockFallbackStrictness(t *testing.T) {
	tempDir := t.TempDir()

	prevMock := models.AppConfig.AI.MockOnMissingCLI
	t.Cleanup(func() { models.AppConfig.AI.MockOnMissingCLI = prevMock })

	// 1. 二进制不存在且 mock 开启（默认）→ 模拟成功并写空发现报告
	enabled := true
	models.AppConfig.AI.MockOnMissingCLI = &enabled
	out1 := filepath.Join(tempDir, "mock.json")
	err := RunCLIProcess("non-existent-ai-cli-xyz", []string{"run"}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "mock test",
		OutputPath: out1,
		TimeoutMin: 1,
	}, "模拟报告")
	if err != nil {
		t.Fatalf("expected mock fallback without error, got %v", err)
	}
	content, err := os.ReadFile(out1)
	if err != nil {
		t.Fatalf("failed to read mock output: %v", err)
	}
	if !strings.Contains(string(content), "findings") {
		t.Fatalf("expected valid mock report, got %s", string(content))
	}

	// 2. mock 关闭 → 返回错误且不写输出文件
	disabled := false
	models.AppConfig.AI.MockOnMissingCLI = &disabled
	out2 := filepath.Join(tempDir, "no-mock.json")
	err = RunCLIProcess("non-existent-ai-cli-xyz", []string{"run"}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "mock disabled test",
		OutputPath: out2,
		TimeoutMin: 1,
	}, "模拟报告")
	if err == nil {
		t.Fatalf("expected error when mock fallback disabled")
	}
	if !strings.Contains(err.Error(), "CLI not installed") {
		t.Fatalf("expected CLI-not-installed error, got %v", err)
	}
	if _, statErr := os.Stat(out2); !os.IsNotExist(statErr) {
		t.Fatalf("mock output should not be written when disabled, stat err=%v", statErr)
	}
}

func TestRunCLIProcess_RuntimeNotFoundIsNotMocked(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "report.json")

	err := RunCLIProcess("sh", []string{"-c", "echo 'error: model not found' >&2; exit 1"}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "test",
		OutputPath: outPath,
		TimeoutMin: 1,
	}, "模拟报告")
	if err == nil {
		t.Fatalf("expected error for failing CLI, mock must not trigger on runtime stderr")
	}
	if !strings.Contains(err.Error(), "AI execution failed") {
		t.Fatalf("expected AI execution failed error, got %v", err)
	}
	// 不得写入模拟报告
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("mock report must not be written for runtime failures, stat err=%v", statErr)
	}
	// stdout 镜像中应记录错误信息
	mirror, readErr := os.ReadFile(outPath + ".output.txt")
	if readErr != nil {
		t.Fatalf("failed to read stdout mirror: %v", readErr)
	}
	if !strings.Contains(string(mirror), "model not found") {
		t.Fatalf("expected error message in stdout mirror, got %q", string(mirror))
	}
}

func TestRunCLIProcess_StdinIsClosed(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "stdin.json")

	// 若 stdin 未关闭，read 会阻塞到 2s 超时；关闭后应立即返回 EOF
	script := "if read -t 2 x; then echo 'GOT_INPUT'; else echo 'EOF_IMMEDIATE'; fi"
	err := RunCLIProcess("sh", []string{"-c", script}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "test",
		OutputPath: outPath,
		TimeoutMin: 1,
	}, "模拟报告")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mirror, readErr := os.ReadFile(outPath + ".output.txt")
	if readErr != nil {
		t.Fatalf("failed to read stdout mirror: %v", readErr)
	}
	if !strings.Contains(string(mirror), "EOF_IMMEDIATE") {
		t.Fatalf("expected immediate EOF from stdin, got %q", string(mirror))
	}
}

func TestRunCLIProcess_ParentCancelKillsProcessGroup(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "cancel.json")

	parentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := RunCLIProcess("sleep", []string{"60"}, AIRequest{
		ParentContext: parentCtx,
		WorkDir:       tempDir,
		PromptMsg:     "test",
		OutputPath:    outPath,
		TimeoutMin:    10,
	}, "模拟报告")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancelled error, got %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunCLIProcess did not return promptly after parent cancel, took %v", elapsed)
	}
	mirror, readErr := os.ReadFile(outPath + ".output.txt")
	if readErr != nil {
		t.Fatalf("failed to read stdout mirror: %v", readErr)
	}
	if !strings.Contains(string(mirror), "cancelled") {
		t.Fatalf("expected cancellation marker in stdout mirror, got %q", string(mirror))
	}
}

func TestRunCLIProcess_SuccessCleansStdoutMirror(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "report.json")

	script := "printf '{\"ok\":true}' > \"$1\""
	err := RunCLIProcess("sh", []string{"-c", script, "sh", outPath}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "test",
		OutputPath: outPath,
		TimeoutMin: 1,
	}, "模拟报告")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stat, statErr := os.Stat(outPath); statErr != nil || stat.Size() == 0 {
		t.Fatalf("expected non-empty output file, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(outPath + ".output.txt"); !os.IsNotExist(statErr) {
		t.Fatalf("stdout mirror should be removed after successful output, stat err=%v", statErr)
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

func TestMergeEnv(t *testing.T) {
	base := []string{"FOO=1", "BAR=2", "BAZ=3"}
	overrides := []string{"BAR=override", "QUX=4"}
	result := mergeEnv(base, overrides)

	resultMap := make(map[string]string)
	for _, e := range result {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			resultMap[parts[0]] = parts[1]
		}
	}

	if resultMap["FOO"] != "1" {
		t.Errorf("expected FOO=1, got %s", resultMap["FOO"])
	}
	if resultMap["BAR"] != "override" {
		t.Errorf("expected BAR=override, got %s", resultMap["BAR"])
	}
	if resultMap["BAZ"] != "3" {
		t.Errorf("expected BAZ=3, got %s", resultMap["BAZ"])
	}
	if resultMap["QUX"] != "4" {
		t.Errorf("expected QUX=4, got %s", resultMap["QUX"])
	}
}

func TestRunCLIProcess_CustomEnv(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "output.txt")
	script := `echo "MY_VAR=$CUSTOM_VAR_TEST" > "$1"`
	err := RunCLIProcess("sh", []string{"-c", script, "sh", outPath}, AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "test",
		OutputPath: outPath,
		TimeoutMin: 1,
		Env:        []string{"CUSTOM_VAR_TEST=hello_shield"},
	}, "模拟报告")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !strings.Contains(string(content), "MY_VAR=hello_shield") {
		t.Fatalf("expected output to contain custom env var, got %s", string(content))
	}
}

func TestSummarizeCLIArgs(t *testing.T) {
	multilinePrompt := `# C/C++ Coredump 风险报告生成指令
你是一个资深代码审计专家。
请仔细检查以下文件是否存在内存越界、空指针等缺陷。
` + strings.Repeat("int a = 0;\n", 100)

	args := []string{
		"run",
		multilinePrompt,
		"--agent",
		"shield-base-scanner",
		"--auto",
		"--format",
		"json",
		"--model",
		"deepseek-v4-flash",
	}

	summary := summarizeCLIArgs(args)

	// 验证结果是单行，不含换行符
	if strings.Contains(summary, "\n") {
		t.Fatalf("summarizeCLIArgs should return single-line summary, got: %s", summary)
	}
	// 验证首行标题被保留
	if !strings.Contains(summary, "# C/C++ Coredump 风险报告生成指令") {
		t.Errorf("expected summary to contain prompt first line title, got: %s", summary)
	}
	// 验证包含数据大小标记
	if !strings.Contains(summary, "KB") && !strings.Contains(summary, "B") {
		t.Errorf("expected summary to contain byte size, got: %s", summary)
	}
	// 验证普通参数保留完整
	if !strings.Contains(summary, `"shield-base-scanner"`) || !strings.Contains(summary, `"deepseek-v4-flash"`) {
		t.Errorf("expected summary to preserve standard arguments, got: %s", summary)
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		bytes int
		want  string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{2560, "2.5KB"},
	}
	for _, tt := range tests {
		got := formatByteSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatByteSize(%d) = %s, want %s", tt.bytes, got, tt.want)
		}
	}
}
