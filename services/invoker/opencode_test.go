package invoker

import (
	"code-shield/models"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	args, cleanup, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}
	defer cleanup()

	argsStr := strings.Join(args, " ")
	for _, expected := range []string{
		"run",
		"--agent shield-base-scanner",
		"--format json",
		"--dir " + tempDir,
		"--model glm5.1",
		"# OpenCode Prompt",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Fatalf("expected args to contain %q, got %v", expected, args)
		}
	}
	if strings.Contains(argsStr, "--auto") {
		t.Fatalf("read-only scanner must not auto-approve permissions: %v", args)
	}
	if strings.Contains(argsStr, "--thinking") {
		t.Fatalf("thinking output must not be enabled for machine contracts: %v", args)
	}
}

func TestOpenCodeInvoker_BuildArgsLargePromptUsesFile(t *testing.T) {
	tempDir := t.TempDir()
	invoker := &OpenCodeInvoker{}
	req := AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  strings.Repeat("x", openCodeInlinePromptMaxBytes+1),
		OutputPath: filepath.Join(tempDir, "output.json"),
		ModelName:  "glm5.1",
	}

	args, cleanup, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs failed: %v", err)
	}
	defer cleanup()

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, strings.Repeat("x", 1024)) {
		t.Fatalf("large prompt was passed through argv: %v", summarizeCLIArgs(args))
	}
	if !strings.Contains(argsStr, "--file") {
		t.Fatalf("expected --file transport, got %v", summarizeCLIArgs(args))
	}
	fileIndex := -1
	for i, arg := range args {
		if arg == "--file" {
			fileIndex = i
			break
		}
	}
	if fileIndex < 0 || fileIndex+1 >= len(args) {
		t.Fatalf("expected --file path, got %v", summarizeCLIArgs(args))
	}
	promptPath := args[fileIndex+1]
	if !filepath.IsAbs(promptPath) {
		t.Fatalf("expected absolute prompt path, got %q", promptPath)
	}
	content, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), strings.Repeat("x", 1024)) {
		t.Fatal("attached prompt file did not contain the complete task payload")
	}
	if strings.Contains(string(content), "Output Delivery") {
		t.Fatal("read-only OpenCode prompt must not ask the agent to write files")
	}
	if !strings.Contains(string(content), "Response Delivery") {
		t.Fatal("attached prompt file did not contain read-only response constraints")
	}
	cleanup()
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatalf("expected prompt file to be removed, got err=%v", err)
	}
}

func TestExtractOpenCodeFinalMessage(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "events.jsonl")
	content := `{"type":"step_start","sessionID":"ses_test"}
{"type":"text","sessionID":"ses_test","part":{"type":"text","messageID":"msg_1","text":"first"}}
{"type":"text","sessionID":"ses_test","part":{"type":"text","messageID":"msg_2","text":"{\"ok\":"}}
{"type":"text","sessionID":"ses_test","part":{"type":"text","messageID":"msg_2","text":"true}"}}
{"type":"step_finish","sessionID":"ses_test"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := extractOpenCodeFinalMessage(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"ok":true}` {
		t.Fatalf("extractOpenCodeFinalMessage = %q, want %q", got, `{"ok":true}`)
	}
}

func TestOpenCodeInvoker_RecoversFinalMessageFromEventStream(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
cat <<'EOF'
{"type":"step_start","sessionID":"ses_test"}
{"type":"text","sessionID":"ses_test","part":{"type":"text","messageID":"msg_test","text":"{\"ok\":true}"}}
{"type":"step_finish","sessionID":"ses_test"}
EOF
`
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(tempDir, "out.json")
	if err := (&OpenCodeInvoker{}).Invoke(AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "analyze",
		OutputPath: outPath,
		TimeoutMin: 1,
	}); err != nil {
		t.Fatalf("expected event-stream output recovery, got %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != `{"ok":true}` {
		t.Fatalf("unexpected recovered output: %q", content)
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

func TestExtractOpenCodeSessionID(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "events.ndjson")
	content := `not json
{"type":"step_start","sessionID":"ses_first"}
{"type":"text","other":"value"}
{"type":"step_start","sessionID":"ses_last"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if got := extractOpenCodeSessionID(path); got != "ses_last" {
		t.Fatalf("extractOpenCodeSessionID() = %q, want %q", got, "ses_last")
	}

	missing := filepath.Join(tempDir, "missing.ndjson")
	if got := extractOpenCodeSessionID(missing); got != "" {
		t.Fatalf("extractOpenCodeSessionID(missing) = %q, want empty", got)
	}
}

func TestOpenCodeContinuationClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "output missing", err: &OpenCodeOutputMissingError{SessionID: "ses"}, want: true},
		{name: "directory output", err: fmt.Errorf("opencode created a directory instead of output file x and no recoverable JSON payload was found"), want: true},
		{name: "idle timeout", err: fmt.Errorf("AI execution idle timed out after 10m0s without stdout/stderr activity"), want: true},
		{name: "execution timeout", err: fmt.Errorf("AI execution timed out after 1h0m0s"), want: true},
		{name: "tool failure", err: fmt.Errorf("AI execution failed: tool call failed"), want: true},
		{name: "cancelled", err: fmt.Errorf("AI execution cancelled"), want: false},
		{name: "safety filter", err: fmt.Errorf("AI model content filter triggered: blocked"), want: false},
		{name: "auth", err: fmt.Errorf("AI execution failed: unauthorized"), want: false},
		{name: "no session", err: &OpenCodeOutputMissingError{OutputPath: "out.json", Cause: "missing"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID := "ses"
			if tt.name == "no session" {
				sessionID = ""
			}
			if got := canContinueAfterOpenCodeFailure(tt.err, CLIProcessResult{SessionID: sessionID}); got != tt.want {
				t.Fatalf("canContinueAfterOpenCodeFailure() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenCodeContinuationBudget(t *testing.T) {
	req := AIRequest{}
	// Default max_seconds is 600. With max-budget semantics, the continuation
	// gets at least the configured max_seconds even when the original request
	// budget has mostly been consumed.
	startedAt := time.Now().Add(-30 * time.Second)
	req.TimeoutMin = 2
	budget, ok := openCodeContinuationBudget(req, startedAt)
	if !ok || budget < 599*time.Second || budget > 600*time.Second {
		t.Fatalf("budget = %v, %v; want between 599s and 600s, true", budget, ok)
	}

	// Even with a very small request budget, the continuation still gets the
	// full configured budget (max, not min).
	req.TimeoutMin = 1
	budget, ok = openCodeContinuationBudget(req, startedAt)
	if !ok || budget < 599*time.Second || budget > 600*time.Second {
		t.Fatalf("budget = %v, %v; want between 599s and 600s, true", budget, ok)
	}

	parentCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req.ParentContext = parentCtx
	req.TimeoutMin = 30
	budget, ok = openCodeContinuationBudget(req, time.Now())
	if !ok || budget < 599*time.Second || budget > 600*time.Second {
		t.Fatalf("parent deadline budget = %v, %v; want between 599s and 600s, true", budget, ok)
	}

	parentCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req.ParentContext = parentCtx
	if _, ok := openCodeContinuationBudget(req, time.Now()); ok {
		t.Fatalf("expected parent budget below minimum to be rejected")
	}
}

func TestOpenCodeInvoker_ContinuesOutputMissingSession(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	outPath := filepath.Join(tempDir, "report.json")
	countFile := filepath.Join(tempDir, "count")
	script := fmt.Sprintf(`#!/bin/sh
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
printf '{"type":"step_start","sessionID":"ses_test"}\n'
if [ "$count" -eq 1 ]; then
  exit 0
fi
case " $* " in
  *" --session ses_test "*) printf '{"ok":true}' > "%s" ;;
  *) exit 42 ;;
esac
`, outPath)
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	prevEnabled := models.AppConfig.Scanner.OpenCode.Continuation.Enabled
	prevMax := models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds
	enabled := true
	models.AppConfig.Scanner.OpenCode.Continuation.Enabled = &enabled
	models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds = 60
	t.Cleanup(func() {
		models.AppConfig.Scanner.OpenCode.Continuation.Enabled = prevEnabled
		models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds = prevMax
	})

	err := (&OpenCodeInvoker{}).Invoke(AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "analyze",
		OutputPath: outPath,
		TimeoutMin: 2,
		Env:        []string{"COUNT_FILE=" + countFile},
	})
	if err != nil {
		t.Fatalf("expected session continuation to recover output, got %v", err)
	}
	content, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != `{"ok":true}` {
		t.Fatalf("unexpected output: %q", content)
	}
	if _, statErr := os.Stat(outPath + ".attempt-1.output.txt"); statErr != nil {
		t.Fatalf("expected first attempt stdout to be preserved: %v", statErr)
	}
}

func TestOpenCodeInvoker_DoesNotContinueWithoutSession(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte("#!/bin/sh\nexit 17\n"), 0755); err != nil {
		t.Fatal(err)
	}

	prevEnabled := models.AppConfig.Scanner.OpenCode.Continuation.Enabled
	enabled := true
	models.AppConfig.Scanner.OpenCode.Continuation.Enabled = &enabled
	t.Cleanup(func() { models.AppConfig.Scanner.OpenCode.Continuation.Enabled = prevEnabled })

	err := (&OpenCodeInvoker{}).Invoke(AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "analyze",
		OutputPath: filepath.Join(tempDir, "missing.json"),
		TimeoutMin: 1,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), "continuation failed") {
		t.Fatalf("unexpected continuation attempt without session: %v", err)
	}
}

func TestOpenCodeInvoker_DisabledContinuationRunsOnce(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	countFile := filepath.Join(tempDir, "count")
	script := fmt.Sprintf(`#!/bin/sh
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
printf '{"type":"step_start","sessionID":"ses_test"}\n'
exit 0
`)
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	prevEnabled := models.AppConfig.Scanner.OpenCode.Continuation.Enabled
	disabled := false
	models.AppConfig.Scanner.OpenCode.Continuation.Enabled = &disabled
	t.Cleanup(func() { models.AppConfig.Scanner.OpenCode.Continuation.Enabled = prevEnabled })

	if err := (&OpenCodeInvoker{}).Invoke(AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "analyze",
		OutputPath: filepath.Join(tempDir, "missing.json"),
		TimeoutMin: 2,
		Env:        []string{"COUNT_FILE=" + countFile},
	}); err == nil {
		t.Fatal("expected output-missing failure when continuation is disabled")
	}

	content, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "1" {
		t.Fatalf("disabled continuation invoked CLI %s times, want 1", string(content))
	}
}

func TestOpenCodeInvoker_ContinuationFailureKeepsFirstAttempt(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '{"type":"step_start","sessionID":"ses_test"}\n'
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	prevEnabled := models.AppConfig.Scanner.OpenCode.Continuation.Enabled
	prevMax := models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds
	enabled := true
	models.AppConfig.Scanner.OpenCode.Continuation.Enabled = &enabled
	models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds = 60
	t.Cleanup(func() {
		models.AppConfig.Scanner.OpenCode.Continuation.Enabled = prevEnabled
		models.AppConfig.Scanner.OpenCode.Continuation.MaxSeconds = prevMax
	})

	outPath := filepath.Join(tempDir, "missing.json")
	err := (&OpenCodeInvoker{}).Invoke(AIRequest{
		WorkDir:    tempDir,
		PromptMsg:  "analyze",
		OutputPath: outPath,
		TimeoutMin: 2,
	})
	if err == nil {
		t.Fatal("expected continuation failure")
	}
	if !strings.Contains(err.Error(), "continuation failed") ||
		!strings.Contains(err.Error(), "first attempt") {
		t.Fatalf("expected combined continuation error, got %v", err)
	}
	if got := ClassifyError(err); got != ErrorClassOutputMissing {
		t.Fatalf("ClassifyError = %q, want output_missing", got)
	}
	if _, statErr := os.Stat(outPath + ".attempt-1.output.txt"); statErr != nil {
		t.Fatalf("expected first attempt artifact: %v", statErr)
	}
}
