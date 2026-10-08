package invoker

import (
	"bufio"
	"code-shield/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// OpenCode needs enough time to replay its session and return the final
	// artifact, but a short bounded budget avoids turning a bad session into
	// another long-running request.
	openCodeContinuationMinSeconds = 60
	// Linux limits a single execve argument to MAX_ARG_STRLEN (32 pages). Stay
	// well below the 128 KiB limit while leaving room for model paths, output
	// paths, and OpenCode flags. Large task specifications are handed to
	// OpenCode through its --file attachment instead of argv.
	openCodeInlinePromptMaxBytes = 96 * 1024
	// openCodePromptFileMessage is intentionally short. The complete task
	// specification (including response delivery and workspace rules) lives in
	// the attached prompt file.
	openCodePromptFileMessage = "Read and strictly execute the complete task specification in the attached file."
)

var (
	openCodeContinuationEligible  atomic.Int64
	openCodeContinuationAttempted atomic.Int64
	openCodeContinuationSucceeded atomic.Int64
	openCodeContinuationFailed    atomic.Int64
)

// OpenCodeContinuationMetrics exposes bounded counters for dispatcher dashboards.
type OpenCodeContinuationMetrics struct {
	Eligible  int64 `json:"eligible"`
	Attempted int64 `json:"attempted"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

// GetOpenCodeContinuationMetrics returns process-lifetime recovery counters.
func GetOpenCodeContinuationMetrics() OpenCodeContinuationMetrics {
	return OpenCodeContinuationMetrics{
		Eligible:  openCodeContinuationEligible.Load(),
		Attempted: openCodeContinuationAttempted.Load(),
		Succeeded: openCodeContinuationSucceeded.Load(),
		Failed:    openCodeContinuationFailed.Load(),
	}
}

// OpenCodeInvoker 使用 opencode CLI 执行 AI 任务。
// 通过全局 shield-base-scanner 基础 Agent 承载通用权限，具体任务规约通过动态 Prompt 注入。
type OpenCodeInvoker struct{}

func (o *OpenCodeInvoker) Name() string { return "opencode" }

// buildArgs 构建 opencode 命令行参数。超过内联限制的 Prompt 通过临时附件文件
// 传输，避免 fork/exec 阶段触发 Linux 的 argument list too long。调用方必须
// 在进程退出后执行返回的 cleanup。
func (o *OpenCodeInvoker) buildArgs(req AIRequest) ([]string, func(), error) {
	userPrompt, err := BuildReadOnlyPromptPayload(req, true)
	if err != nil {
		return nil, func() {}, err
	}

	// OpenCode JSON event mode is required to recover the session ID and final
	// assistant message. The business artifact is materialized by this service.
	formatVal := "json"
	promptMessage := userPrompt
	promptFilePath := ""
	promptFileCleanup := func() {}

	if len(userPrompt) > openCodeInlinePromptMaxBytes {
		path, cleanup, err := writeOpenCodePromptFile(userPrompt)
		if err != nil {
			return nil, func() {}, err
		}
		promptFilePath = path
		promptFileCleanup = cleanup
		promptMessage = openCodePromptFileMessage
		log.Printf("[OpenCode] Large prompt switched to file transport: size=%d bytes\n", len(userPrompt))
	}

	args := []string{
		"run", promptMessage,
		"--agent", BaseScannerAgentName,
		"--format", formatVal,
	}
	if promptFilePath != "" {
		args = append(args, "--file", promptFilePath)
	}

	if req.WorkDir != "" {
		args = append(args, "--dir", req.WorkDir)
	}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

	if models.AppConfig.AI.DebugLogs {
		args = append(args, "--print-logs", "--log-level", "INFO")
	}

	return args, promptFileCleanup, nil
}

func writeOpenCodePromptFile(payload string) (string, func(), error) {
	file, err := os.CreateTemp("", "code-shield-opencode-prompt-*.md")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create opencode prompt file: %w", err)
	}
	path, err := filepath.Abs(file.Name())
	if err != nil {
		file.Close()
		_ = os.Remove(file.Name())
		return "", func() {}, fmt.Errorf("failed to resolve opencode prompt file path: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(path)
	}

	if _, err := file.WriteString(payload); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("failed to write opencode prompt file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("failed to sync opencode prompt file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("failed to close opencode prompt file: %w", err)
	}

	return path, cleanup, nil
}

// prepareIsolatedDataDir 为 OpenCode 准备独立的 XDG_DATA_HOME 目录，避免多进程并发调用时的 SQLite 数据库锁（database is locked）冲突
func prepareIsolatedDataDir() (string, func(), error) {
	tempDataDir, err := os.MkdirTemp("", "opencode-data-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to create temp opencode data dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDataDir)
	}

	opencodeDir := filepath.Join(tempDataDir, "opencode")
	if err := os.MkdirAll(opencodeDir, 0755); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("failed to create isolated opencode dir: %w", err)
	}

	// 共享/继承原有 auth.json 认证凭据
	srcAuthPath := ""
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		candidate := filepath.Join(xdgData, "opencode", "auth.json")
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			srcAuthPath = candidate
		}
	}
	if srcAuthPath == "" {
		if home, hErr := os.UserHomeDir(); hErr == nil {
			candidate := filepath.Join(home, ".local", "share", "opencode", "auth.json")
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				srcAuthPath = candidate
			}
		}
	}
	if srcAuthPath != "" {
		destAuthPath := filepath.Join(opencodeDir, "auth.json")
		if symErr := os.Symlink(srcAuthPath, destAuthPath); symErr != nil {
			if content, rErr := os.ReadFile(srcAuthPath); rErr == nil {
				_ = os.WriteFile(destAuthPath, content, 0600)
			}
		}
	}

	return tempDataDir, cleanup, nil
}

// Invoke 调用 opencode run 执行 AI 任务
func (o *OpenCodeInvoker) Invoke(req AIRequest) error {
	if err := EnsureBaseAgent(); err != nil {
		return fmt.Errorf("failed to ensure base agent: %w", err)
	}

	startedAt := time.Now()
	args, promptFileCleanup, err := o.buildArgs(req)
	if err != nil {
		return err
	}
	defer promptFileCleanup()

	tempDataDir, cleanup, err := prepareIsolatedDataDir()
	if err != nil {
		return err
	}
	defer cleanup()

	req.Env = append(req.Env, fmt.Sprintf("XDG_DATA_HOME=%s", tempDataDir))

	result, firstErr := RunCLIProcessWithResult(
		"opencode", args, req, "模拟报告：OpenCode AI 引擎未安装",
	)
	if firstErr == nil {
		return nil
	}

	if !models.AppConfig.OpenCodeContinuationEnabled() ||
		!canContinueAfterOpenCodeFailure(firstErr, result) {
		return firstErr
	}
	openCodeContinuationEligible.Add(1)

	budget, ok := openCodeContinuationBudget(req, startedAt)
	if !ok {
		return firstErr
	}
	openCodeContinuationAttempted.Add(1)
	if req.Metrics != nil {
		req.Metrics.Continuations++
	}

	retryCtx, cancel := context.WithTimeout(parentOrDefault(req.ParentContext), budget)
	defer cancel()

	preserveOpenCodeAttemptArtifacts(req.OutputPath)
	retryReq := req
	retryReq.ParentContext = retryCtx
	retryReq.PromptFile = ""
	retryReq.InputFiles = nil
	retryReq.PromptMsg = buildOpenCodeContinuationPrompt()
	retryReq.TimeoutMin = int(budget.Minutes())
	if remainingSeconds := int(budget.Seconds()); remainingSeconds%60 != 0 {
		retryReq.TimeoutMin++
	}

	retryArgs, retryPromptFileCleanup, err := o.buildArgs(retryReq)
	if err != nil {
		openCodeContinuationFailed.Add(1)
		return wrapOpenCodeContinuationError(err, firstErr)
	}
	defer retryPromptFileCleanup()
	retryArgs = append(retryArgs, "--session", result.SessionID)

	if _, retryErr := RunCLIProcessWithResult(
		"opencode", retryArgs, retryReq, "模拟报告：OpenCode AI 引擎未安装",
	); retryErr != nil {
		openCodeContinuationFailed.Add(1)
		return wrapOpenCodeContinuationError(retryErr, firstErr)
	}

	openCodeContinuationSucceeded.Add(1)
	log.Printf("[OpenCode] Recovered output through session continuation: %s\n", req.OutputPath)
	return nil
}

func wrapOpenCodeContinuationError(retryErr, firstErr error) error {
	if retryErr == nil {
		return nil
	}
	// The historical fmt.Errorf wrapper lost the typed error class. Callers
	// use ErrorClassOutputMissing to decide whether a fresh-session retry and
	// bundle bisection are safe, so the retry cause must remain discoverable.
	wrapped := fmt.Errorf("opencode continuation failed: %w; first attempt: %s", retryErr, firstErr.Error())
	return WrapClassifiedError(ClassifyError(retryErr), wrapped, "%s", wrapped.Error())
}

func parentOrDefault(parent context.Context) context.Context {
	if parent != nil {
		return parent
	}
	return context.Background()
}

func openCodeContinuationBudget(req AIRequest, startedAt time.Time) (time.Duration, bool) {
	maxBudget := time.Duration(models.AppConfig.OpenCodeContinuationMaxSeconds()) * time.Second
	budget := maxBudget

	// Respect the original AIRequest budget even when its parent context has
	// no deadline. This prevents a continuation from doubling the request's
	// configured wall-clock limit after a timeout.
	requestBudget := time.Duration(req.TimeoutMin) * time.Minute
	if requestBudget <= 0 {
		requestBudget = defaultCLITimeout
	}
	// Use the larger of the configured continuation budget and the remaining
	// request budget. This prevents the continuation from being squeezed to a
	// tiny sliver just because the first attempt consumed most of the request
	// budget. The parent context deadline (checked below) still acts as the
	// authoritative hard limit.
	if remaining := requestBudget - time.Since(startedAt); remaining > budget {
		budget = remaining
	}
	budget = budget.Truncate(time.Second)

	if parent := req.ParentContext; parent != nil {
		if deadline, ok := parent.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return 0, false
			}
			if remaining < budget {
				budget = remaining
			}
		}
	}

	if budget < openCodeContinuationMinSeconds*time.Second {
		return 0, false
	}
	return budget, true
}

func preserveOpenCodeAttemptArtifacts(outputPath string) {
	if outputPath == "" {
		return
	}
	for _, suffix := range []string{".output.txt", ".debug.log"} {
		source := outputPath + suffix
		destination := outputPath + ".attempt-1" + suffix
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			log.Printf("[OpenCode] Failed to remove stale attempt artifact %s: %v\n", destination, err)
		}
		if err := os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
			log.Printf("[OpenCode] Failed to preserve attempt artifact %s: %v\n", source, err)
		}
	}
}

func buildOpenCodeContinuationPrompt() string {
	return `继续上一个会话。前面的分析结果已经存在，不要重新分析，不要重新读取全部文件，不要创建、写入、修改或删除文件。

请立即根据前面的分析结论生成最终 JSON，并直接作为最后一条消息返回。

硬性要求：
1. 内容只能是符合原任务输出契约的 UTF-8 JSON。
2. 不得包含 Markdown 代码围栏、解释文字、日志或思考过程。`
}

func extractOpenCodeFinalMessage(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var messageOrder []string
	messages := make(map[string]*strings.Builder)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Part *struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				MessageID string `json:"messageID"`
			} `json:"part"`
		}
		if jsonErr := json.Unmarshal(scanner.Bytes(), &event); jsonErr != nil {
			continue
		}
		if event.Type != "text" || event.Part == nil || event.Part.Type != "text" || strings.TrimSpace(event.Part.Text) == "" {
			continue
		}
		messageID := event.Part.MessageID
		if messageID == "" {
			messageID = "__last__"
		}
		builder, exists := messages[messageID]
		if !exists {
			builder = &strings.Builder{}
			messages[messageID] = builder
			messageOrder = append(messageOrder, messageID)
		}
		builder.WriteString(event.Part.Text)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", scanErr
	}
	if len(messageOrder) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	return strings.TrimSpace(messages[messageOrder[len(messageOrder)-1]].String()), nil
}

func canContinueAfterOpenCodeFailure(err error, result CLIProcessResult) bool {
	if err == nil || result.SessionID == "" {
		return false
	}

	// The typed guard is authoritative and its sample text must not be used to
	// infer an unrelated auth/tool failure from a large transcript excerpt.
	if isOutputMissingError(err) {
		return true
	}
	switch ClassifyError(err) {
	case ErrorClassCanceled, ErrorClassAuth, ErrorClassContentFiltered:
		return false
	case ErrorClassIdleTimeout, ErrorClassTimeout:
		return true
	case ErrorClassNetworkTransient:
		return true
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cancelled") {
		return false
	}
	if blocked, _ := isSafetyFilterBlocked(err.Error()); blocked {
		return false
	}
	if isAuthOrModelUnavailableError(message) {
		return false
	}

	return isIdleTimeoutError(message) ||
		isExecutionTimeoutError(message) ||
		isTransientToolFailureError(message)
}

func isOutputMissingError(err error) bool {
	var target *OpenCodeOutputMissingError
	if errors.As(err, &target) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no recoverable json payload") ||
		strings.Contains(message, "instead of output file")
}

func isIdleTimeoutError(message string) bool {
	return strings.Contains(message, "idle timed out")
}

func isExecutionTimeoutError(message string) bool {
	return strings.Contains(message, "ai execution timed out")
}

func isTransientToolFailureError(message string) bool {
	for _, marker := range []string{
		"tool call failed",
		"tool execution failed",
		"failed to write",
		"network",
		"connection reset",
		"connection refused",
		"temporary failure",
		"temporarily unavailable",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isAuthOrModelUnavailableError(message string) bool {
	for _, marker := range []string{
		"unauthorized",
		"authentication",
		"invalid api key",
		"api key",
		"model not found",
		"provider not found",
		"quota exceeded",
		"rate limit",
		"billing",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func init() {
	RegisterAIInvoker("opencode", &OpenCodeInvoker{})
}
