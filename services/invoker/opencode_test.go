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

	prevFormat := models.AppConfig.Scanner.OutputFormat
	prevDebug := models.AppConfig.LLM.DebugLogs
	t.Cleanup(func() {
		models.AppConfig.Scanner.OutputFormat = prevFormat
		models.AppConfig.LLM.DebugLogs = prevDebug
	})
	models.AppConfig.Scanner.OutputFormat = "json"
	models.AppConfig.LLM.DebugLogs = false

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
		"--standalone",
		"--agent shield-base-scanner",
		"--format json",
		"--archived",
		"--model glm5.1",
		"--",
		"# OpenCode Prompt",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Fatalf("expected args to contain %q, got %v", expected, args)
		}
	}
	for _, forbidden := range []string{"--dir", "--auto", "--thinking", "--print-logs", "--log-level"} {
		if strings.Contains(argsStr, forbidden) {
			t.Fatalf("unexpected flag %q in args: %v", forbidden, args)
		}
	}
	if len(args) < 3 || args[len(args)-2] != "--" {
		t.Fatalf("expected '--' immediately before final prompt argument, got %v", args)
	}

	// DebugLogs=true 时应使用小写 --log-level info
	models.AppConfig.LLM.DebugLogs = true
	debugArgs, debugCleanup, err := invoker.buildArgs(req)
	if err != nil {
		t.Fatalf("buildArgs(debug) failed: %v", err)
	}
	defer debugCleanup()
	debugStr := strings.Join(debugArgs, " ")
	if !strings.Contains(debugStr, "--print-logs --log-level info") {
		t.Fatalf("expected lowercase '--print-logs --log-level info', got %v", debugArgs)
	}
	if strings.Contains(debugStr, "--log-level INFO") {
		t.Fatalf("must not use uppercase INFO log level in v2.x: %v", debugArgs)
	}

	// 续跑会话应把 --session 放在 -- 之前，且不传 --archived
	models.AppConfig.LLM.DebugLogs = false
	dashReq := AIRequest{
		WorkDir:   tempDir,
		PromptMsg: "--- lead-with-dashes",
		ModelName: "glm5.1",
	}
	resumeArgs, resumeCleanup, err := invoker.buildArgsForSession(dashReq, "ses_resume")
	if err != nil {
		t.Fatalf("buildArgsForSession failed: %v", err)
	}
	defer resumeCleanup()
	resumeStr := strings.Join(resumeArgs, " ")
	if strings.Contains(resumeStr, "--archived") {
		t.Fatalf("resumed session must not include --archived: %v", resumeArgs)
	}
	if !strings.Contains(resumeStr, "--session ses_resume") {
		t.Fatalf("resumed session missing --session ses_resume: %v", resumeArgs)
	}
	if len(resumeArgs) < 3 || resumeArgs[len(resumeArgs)-2] != "--" || !strings.HasPrefix(resumeArgs[len(resumeArgs)-1], "--- lead-with-dashes") {
		t.Fatalf("prompt starting with dashes must follow '--', got %v", resumeArgs)
	}
	sessIdx, sepIdx := -1, len(resumeArgs)-2
	for i, a := range resumeArgs {
		if a == "--session" {
			sessIdx = i
			break
		}
	}
	if sessIdx < 0 || sessIdx >= sepIdx {
		t.Fatalf("--session must precede '--': %v", resumeArgs)
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
	expectedDir := filepath.Join(os.TempDir(), fmt.Sprintf("opencode-%d", os.Getuid()))
	if filepath.Dir(promptPath) != expectedDir {
		t.Fatalf("expected prompt file in %q, got %q", expectedDir, promptPath)
	}
	stat, err := os.Stat(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0644 {
		t.Fatalf("expected file permission 0644, got %v", stat.Mode().Perm())
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
	t.Setenv("XDG_CONFIG_HOME", "")

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
	for _, requiredRule := range []string{
		"permission:",
		`"*": deny`,
		`"*.env": deny`,
		"shell: deny",
		"external_directory: deny",
		"webfetch: deny",
	} {
		if !strings.Contains(string(content), requiredRule) {
			t.Fatalf("base agent missing strict v2 permission rule %q:\n%s", requiredRule, string(content))
		}
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

	// 显式设置 XDG_CONFIG_HOME 时应写入对应目录
	xdgCfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgCfg)
	if err := EnsureBaseAgent(); err != nil {
		t.Fatalf("EnsureBaseAgent (XDG_CONFIG_HOME) failed: %v", err)
	}
	xdgAgentPath := filepath.Join(xdgCfg, "opencode", "agents", BaseScannerAgentName+".md")
	if xdgContent, readErr := os.ReadFile(xdgAgentPath); readErr != nil || string(xdgContent) != BaseScannerAgentContent {
		t.Fatalf("expected base agent under XDG_CONFIG_HOME, err=%v", readErr)
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
		{name: "rate limited", err: NewClassifiedError(ErrorClassRateLimited, "rate limit exceeded"), want: true},
		{name: "network transient", err: NewClassifiedError(ErrorClassNetworkTransient, "connection reset by peer"), want: true},
		{name: "unknown with session", err: NewClassifiedError(ErrorClassUnknown, "unexpected runtime error"), want: true},
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg_cfg"))

	outPath := filepath.Join(tempDir, "report.json")
	countFile := filepath.Join(tempDir, "count")
	metaLog := filepath.Join(tempDir, "invocations.log")
	script := fmt.Sprintf(`#!/bin/sh
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
args_one_line=$(printf '%%s ' "$@" | tr '\n' ' ')
printf '%%s|PWD=%%s|DATA=%%s|STATE=%%s|CACHE=%%s|ARGS=%%s\n' "$count" "$PWD" "$XDG_DATA_HOME" "$XDG_STATE_HOME" "$XDG_CACHE_HOME" "$args_one_line" >> "$META_LOG"
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
		Env:        []string{"COUNT_FILE=" + countFile, "META_LOG=" + metaLog},
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

	metaBytes, readMetaErr := os.ReadFile(metaLog)
	if readMetaErr != nil {
		t.Fatal(readMetaErr)
	}
	lines := strings.Split(strings.TrimSpace(string(metaBytes)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 CLI invocations, got %d:\n%s", len(lines), string(metaBytes))
	}
	if !strings.Contains(lines[0], "--standalone") || !strings.Contains(lines[0], "--archived") {
		t.Fatalf("attempt 1 missing --standalone or --archived: %s", lines[0])
	}
	if !strings.Contains(lines[1], "--standalone") || !strings.Contains(lines[1], "--session ses_test") || strings.Contains(lines[1], "--archived") {
		t.Fatalf("attempt 2 args mismatch: %s", lines[1])
	}
	if !strings.Contains(lines[0], "PWD="+tempDir) || !strings.Contains(lines[1], "PWD="+tempDir) {
		t.Fatalf("expected PWD=%s in both attempts:\n%s", tempDir, string(metaBytes))
	}
	data1 := extractField(lines[0], "DATA=")
	data2 := extractField(lines[1], "DATA=")
	state1 := extractField(lines[0], "STATE=")
	state2 := extractField(lines[1], "STATE=")
	cache1 := extractField(lines[0], "CACHE=")
	cache2 := extractField(lines[1], "CACHE=")
	if data1 == "" || data1 != data2 || state1 == "" || state1 != state2 || cache1 == "" || cache1 != cache2 {
		t.Fatalf("expected identical non-empty XDG dirs across attempts:\n%s", string(metaBytes))
	}
}

func extractField(line, prefix string) string {
	for _, part := range strings.Split(line, "|") {
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
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

func TestPrepareOpenCodeRuntimeEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	authDir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	authContent := `{"provider":{"key":"k"}}`
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(authContent), 0600); err != nil {
		t.Fatalf("failed to write auth.json: %v", err)
	}

	workDir := t.TempDir()
	env, cleanup, err := prepareOpenCodeRuntimeEnv(workDir)
	if err != nil {
		t.Fatalf("prepareOpenCodeRuntimeEnv failed: %v", err)
	}

	envMap := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	for _, key := range []string{
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
		"XDG_CACHE_HOME",
		"CI",
		"NO_COLOR",
		"TERM",
		"OPENCODE_DISABLE_PROJECT_CONFIG",
		"PWD",
	} {
		if envMap[key] == "" {
			t.Fatalf("missing env var %s in %v", key, envMap)
		}
	}
	if envMap["PWD"] != workDir {
		t.Fatalf("PWD = %q, want %q", envMap["PWD"], workDir)
	}
	if envMap["CI"] != "true" || envMap["NO_COLOR"] != "1" || envMap["TERM"] != "dumb" {
		t.Fatalf("unexpected non-interactive env flags: %v", envMap)
	}

	isolatedAuth := filepath.Join(envMap["XDG_DATA_HOME"], "opencode", "auth.json")
	gotAuth, err := os.ReadFile(isolatedAuth)
	if err != nil || string(gotAuth) != authContent {
		t.Fatalf("auth.json mismatch: got %q, err=%v", string(gotAuth), err)
	}

	rootDir := filepath.Dir(envMap["XDG_DATA_HOME"])
	cleanup()
	if _, err := os.Stat(rootDir); !os.IsNotExist(err) {
		t.Fatalf("expected runtime root %s to be deleted after cleanup, got err=%v", rootDir, err)
	}
}

func TestParseOpenCodeEventStream(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "events.jsonl")
	hugeToolLine := fmt.Sprintf(`{"type":"tool_use","sessionID":"ses_v2","part":{"type":"tool","state":{"output":"%s"}}}`, strings.Repeat("a", 2*1024*1024))
	content := strings.Join([]string{
		`{"type":"step_start","sessionID":"ses_v2"}`,
		hugeToolLine,
		`{"type":"text","sessionID":"ses_v2","part":{"type":"text","messageID":"msg_1","text":"{\"res\":"}}`,
		`{"type":"text","sessionID":"ses_v2","part":{"type":"text","messageID":"msg_1","text":"1}"}}`,
		`{"type":"step_finish","sessionID":"ses_v2","part":{"type":"step-finish","tokens":{"input":100,"output":25,"reasoning":15,"cache":{"read":500,"write":0}}}}`,
		`{"type":"error","sessionID":"ses_v2","error":{"type":"provider.no-route","message":"Model unavailable: bad/model"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	summary, err := parseOpenCodeEventStream(path)
	if err != nil {
		t.Fatalf("parseOpenCodeEventStream failed: %v", err)
	}
	if summary.SessionID != "ses_v2" {
		t.Fatalf("SessionID = %q, want ses_v2", summary.SessionID)
	}
	if summary.FinalMessage != `{"res":1}` {
		t.Fatalf("FinalMessage = %q, want {\"res\":1}", summary.FinalMessage)
	}
	if summary.TokenUsage == nil ||
		summary.TokenUsage.InputTokens != 600 ||
		summary.TokenUsage.CachedInputTokens != 500 ||
		summary.TokenUsage.OutputTokens != 25 ||
		summary.TokenUsage.ReasoningOutputTokens != 15 ||
		summary.TokenUsage.TotalTokens != 625 {
		t.Fatalf("unexpected TokenUsage: %+v", summary.TokenUsage)
	}
	if len(summary.Errors) != 1 || !strings.Contains(summary.Errors[0], "provider.no-route: Model unavailable: bad/model") {
		t.Fatalf("unexpected stream errors: %+v", summary.Errors)
	}
}

func TestOpenCodeInvoker_ModelUnavailableClassifiedAsAuthWithoutContinuation(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg_cfg"))

	countFile := filepath.Join(tempDir, "count")
	script := `#!/bin/sh
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
printf '{"type":"error","sessionID":"ses_err","error":{"type":"provider.no-route","message":"Model unavailable: bad/model"}}\n'
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	prevEnabled := models.AppConfig.Scanner.OpenCode.Continuation.Enabled
	enabled := true
	models.AppConfig.Scanner.OpenCode.Continuation.Enabled = &enabled
	t.Cleanup(func() { models.AppConfig.Scanner.OpenCode.Continuation.Enabled = prevEnabled })

	outPath := filepath.Join(tempDir, "out.json")
	req := AIRequest{
		WorkDir:       tempDir,
		PromptMsg:     "analyze",
		OutputPath:    outPath,
		TimeoutMin:    2,
		Env:           []string{"COUNT_FILE=" + countFile},
		Observability: NewCallObservability(AIRequest{OutputPath: outPath}, "opencode", "opencode"),
	}
	err := (&OpenCodeInvoker{}).Invoke(req)
	if err == nil {
		t.Fatal("expected error for unavailable model")
	}
	if got := ClassifyError(err); got != ErrorClassAuth {
		t.Fatalf("ClassifyError = %q, want %q (err=%v)", got, ErrorClassAuth, err)
	}
	if strings.Contains(err.Error(), "continuation failed") {
		t.Fatalf("model unavailable must not trigger continuation: %v", err)
	}
	countBytes, readErr := os.ReadFile(countFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.TrimSpace(string(countBytes)) != "1" {
		t.Fatalf("CLI invoked %s times, want 1", strings.TrimSpace(string(countBytes)))
	}
	diag := req.Observability.CLIDiagnosticsSnapshot()
	if diag == nil || !strings.Contains(diag.StderrTail, "Model unavailable: bad/model") {
		t.Fatalf("expected stream error in CLIDiagnostics.StderrTail, got %+v", diag)
	}
}
