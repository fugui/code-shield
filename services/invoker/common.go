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
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// defaultCLITimeout AI CLI 执行默认超时（与 AIRequest.TimeoutMin 注释保持一致）
	defaultCLITimeout = 60 * time.Minute
	// killReapTimeout SIGKILL 后等待进程退出的二次兜底时限
	killReapTimeout = 10 * time.Second
)

// BuildPromptPayload 组装通用的 Prompt 规约、分片提示及输入文件清单
// includePromptFile: 是否将 PromptFile 内容内联拼接到消息前部（OpenCode 与 Codex 设为 true）
func BuildPromptPayload(req AIRequest, includePromptFile bool) (string, error) {
	var sb strings.Builder

	if includePromptFile && req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return "", fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
		promptBytes, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt file: %w", err)
		}
		sb.WriteString(string(promptBytes))
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString(fmt.Sprintf("%s\n\n", req.PromptMsg))

	if req.OutputPath != "" {
		sb.WriteString("## Output Delivery\n")
		sb.WriteString(fmt.Sprintf("将最终结果写入普通文件：%s\n", req.OutputPath))
		sb.WriteString("硬性要求：目标路径必须是普通文件；不得创建同名目录；不得把结果写入其子目录。\n")
		sb.WriteString("文件内容只能是 UTF-8 JSON 或任务指定的 Markdown；不得包含 BOM、注释、Markdown 代码围栏、日志或解释文字。\n")
		sb.WriteString("如果目标文件已存在，直接覆盖。不得修改业务源码。\n\n")
	}

	if req.WorkDir != "" {
		sb.WriteString(fmt.Sprintf("## Workspace\n当前分析运行目录为代码仓根目录：%s，待分析文件路径均为相对于该根目录的相对路径。\n\n", req.WorkDir))
	}

	if len(req.InputFiles) > 0 {
		if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json.raw") {
			sb.WriteString("任务采用分片执行，本次只分析下列输入文件。\n")
		}
		sb.WriteString("## Additional Input Files\n")
		for _, file := range req.InputFiles {
			sb.WriteString(fmt.Sprintf("- `%s`\n", file))
		}
		sb.WriteString("\n")
	} else if includePromptFile {
		sb.WriteString("请分析 Prompt 上方 Target Files / 待检视文件列表中的文件。\n")
	} else {
		sb.WriteString("请基于任务指令和上下文中的文件清单进行分析。\n")
	}

	return sb.String(), nil
}

// BuildReadOnlyPromptPayload assembles a prompt for agents whose execution
// sandbox forbids filesystem writes. The business artifact path is still
// supplied through AIRequest.OutputPath and is materialized by this service,
// never by the agent.
func BuildReadOnlyPromptPayload(req AIRequest, includePromptFile bool) (string, error) {
	readOnlyReq := req
	readOnlyReq.OutputPath = ""
	payload, err := BuildPromptPayload(readOnlyReq, includePromptFile)
	if err != nil {
		return "", err
	}

	if req.OutputPath != "" {
		payload += `

## Response Delivery
严禁创建、写入、修改或删除任何文件。
不要执行 shell 重定向、tee、touch、编辑器或任何写盘工具。
将完整最终结果作为最后一条消息直接返回；内容只能是任务要求的 UTF-8 JSON 或 Markdown。
不得包含 BOM、Markdown 代码围栏、注释、日志、审批说明或解释文字。
`
	}
	return payload, nil
}

// isExecutableNotFound 严格判定 CLI 可执行文件不存在的启动错误。
// 仅匹配 exec.ErrNotFound，避免将 CLI 运行期报错中出现的 "not found" 误判为未安装。
func isExecutableNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// writeMockReport 写入 CLI 未安装时的模拟报告（仅在配置允许且严格判定为未安装时触发）
func writeMockReport(cliName string, req AIRequest, mockSummary string) error {
	log.Printf("[%s] WARNING: simulating success (%s CLI not installed), report is a MOCK with zero findings\n",
		cliName, cliName)
	mockPayload := fmt.Sprintf(`{"findings":[],"summary":"%s"}`, mockSummary)
	if writeErr := os.WriteFile(req.OutputPath, []byte(mockPayload), 0644); writeErr != nil {
		return fmt.Errorf("failed to write mock report: %w", writeErr)
	}
	return nil
}

// appendMetaError 向 metaFile 追加错误标记，写入失败仅记录日志（不静默忽略）
func appendMetaError(cliName string, metaFile *os.File, msg string) {
	if _, writeErr := metaFile.WriteString("\n\n[Code-Shield Error] " + msg + "\n"); writeErr != nil {
		log.Printf("[%s] Failed to write error to metaFile: %v\n", cliName, writeErr)
	}
}

// truncateString 按字节截断长字符串并追加截断标记
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + " ...[truncated]"
}

// cleanInvocationArtifacts removes files written by a previous attempt for the
// same exact output path. Without this, diagnostics can show a completed JSON
// artifact while the current thick-agent process is still running.
func cleanInvocationArtifacts(outputPath string) {
	if outputPath == "" {
		return
	}
	for _, path := range []string{
		outputPath,
		outputPath + ".output.txt",
		outputPath + ".debug.log",
		outputPath + ".lastmsg",
	} {
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[Invoker] Failed to clean invocation artifact %s: %v\n", path, err)
		}
	}
}

// mergeEnv 将 overrides 覆盖合并到 base 环境变量列表中
func mergeEnv(base []string, overrides []string) []string {
	if len(overrides) == 0 {
		return base
	}
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, e := range overrides {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) > 0 {
			overrideKeys[parts[0]] = struct{}{}
		}
	}
	var res []string
	for _, e := range base {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) > 0 {
			if _, ok := overrideKeys[parts[0]]; !ok {
				res = append(res, e)
			}
		}
	}
	res = append(res, overrides...)
	return res
}

// summarizeCLIArgs 将命令行参数格式化为单行紧凑摘要，截断多行提示词及超长文本，保持终端输出清爽
func summarizeCLIArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if strings.Contains(trimmed, "\n") || len([]rune(trimmed)) > 60 {
			firstLine := trimmed
			if idx := strings.Index(trimmed, "\n"); idx != -1 {
				firstLine = strings.TrimSpace(trimmed[:idx])
			}
			runes := []rune(firstLine)
			if len(runes) > 40 {
				firstLine = string(runes[:40]) + "..."
			}
			sizeStr := formatByteSize(len(arg))
			if firstLine != "" {
				parts = append(parts, fmt.Sprintf("%q... (%s)", firstLine, sizeStr))
			} else {
				parts = append(parts, fmt.Sprintf("<payload %s>", sizeStr))
			}
		} else {
			parts = append(parts, fmt.Sprintf("%q", trimmed))
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// formatByteSize 格式化字节大小为可读字符串 (如 8.5KB)
func formatByteSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	return fmt.Sprintf("%.1fKB", float64(bytes)/1024.0)
}

// activityWriter records every process output write as liveness evidence.
type activityWriter struct {
	delegate io.Writer
	activity func()
}

func (w *activityWriter) Write(p []byte) (int, error) {
	if w.activity != nil {
		w.activity()
	}
	return w.delegate.Write(p)
}

// CLIProcessResult exposes driver-specific metadata that survives a managed
// CLI invocation. OpenCode emits a stable session ID in its JSON event stream.
type CLIProcessResult struct {
	SessionID         string
	ExitCode          *int
	TerminationReason string
	TokenUsage        *TokenUsage
}

// OpenCodeOutputMissingError marks the recoverable case where OpenCode exited
// successfully but did not produce a non-empty regular output file.
type OpenCodeOutputMissingError struct {
	SessionID  string
	OutputPath string
	Cause      string
}

func (e *OpenCodeOutputMissingError) Error() string {
	return e.Cause
}

// extractOpenCodeSessionID scans the bounded stdout mirror without loading the
// entire thick-agent transcript into memory.
func extractOpenCodeSessionID(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	var sessionID string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.SessionID != "" {
			sessionID = event.SessionID
		}
	}
	return sessionID
}

// RunCLIProcess keeps the historical convenience API used by other drivers and
// callers that do not need driver-specific continuation metadata.
func RunCLIProcess(cliName string, args []string, req AIRequest, mockSummary string) error {
	_, err := RunCLIProcessWithResult(cliName, args, req, mockSummary)
	return err
}

// RunCLIProcessWithResult 统一管理所有 AI CLI 的执行、超时、进程组治理与 Mock 降级。
func RunCLIProcessWithResult(cliName string, args []string, req AIRequest, mockSummary string) (CLIProcessResult, error) {
	result := CLIProcessResult{}
	var timeout time.Duration
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	} else if req.TimeoutMin > 0 {
		timeout = time.Duration(req.TimeoutMin) * time.Minute
	} else {
		timeout = defaultCLITimeout
	}
	idleTimeout := time.Duration(req.IdleTimeoutSeconds) * time.Second

	parentCtx := req.ParentContext
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctxRun, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	if req.WorkDir != "" {
		absDir, err := filepath.Abs(req.WorkDir)
		if err != nil {
			return result, fmt.Errorf("invalid workdir %q: %w", req.WorkDir, err)
		}
		info, err := os.Stat(absDir)
		if err != nil || !info.IsDir() {
			return result, fmt.Errorf("workdir does not exist or is not a directory: %s", absDir)
		}
		req.WorkDir = absDir
	}

	cleanInvocationArtifacts(req.OutputPath)

	cliOutputPath := req.OutputPath + ".output.txt"
	metaFile, err := os.Create(cliOutputPath)
	if err != nil {
		return result, fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	log.Printf("[%s] WorkDir: %s, Args: %s\n", cliName, req.WorkDir, summarizeCLIArgs(args))

	var stderrBuf strings.Builder
	var lastActivityNano atomic.Int64
	lastActivityNano.Store(time.Now().UnixNano())
	activity := func() { lastActivityNano.Store(time.Now().UnixNano()) }
	newActivityWriter := func(w io.Writer) io.Writer {
		return &activityWriter{delegate: w, activity: activity}
	}

	cmd := exec.Command(cliName, args...)
	cmd.Dir = req.WorkDir
	if req.StdinPayload != "" {
		cmd.Stdin = strings.NewReader(req.StdinPayload)
	} else {
		cmd.Stdin = strings.NewReader("")
	}
	stdoutWriter := newActivityWriter(metaFile)
	stderrWriter := newActivityWriter(&stderrBuf)
	if req.Observability != nil {
		if consoleWriter := req.Observability.ConsoleWriter("stdout"); consoleWriter != nil {
			stdoutWriter = newActivityWriter(io.MultiWriter(metaFile, consoleWriter))
		}
	}
	cmd.Stdout = stdoutWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if len(req.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), req.Env)
	}

	var debugLogFile *os.File
	debugLogPath := req.OutputPath + ".debug.log"

	if models.AppConfig.AI.DebugLogs {
		debugLogFile, err = os.Create(debugLogPath)
		if err == nil {
			defer debugLogFile.Close()
			stderrWriter = newActivityWriter(io.MultiWriter(&stderrBuf, debugLogFile))
		} else {
			log.Printf("[%s] Failed to create debug log file %s: %v\n", cliName, debugLogPath, err)
		}
	}
	if req.Observability != nil {
		if consoleWriter := req.Observability.ConsoleWriter("stderr"); consoleWriter != nil {
			if stderrWriter == nil {
				stderrWriter = consoleWriter
			} else {
				stderrWriter = newActivityWriter(io.MultiWriter(stderrWriter, consoleWriter))
			}
		}
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		if isExecutableNotFound(err) {
			if models.AppConfig.MockOnMissingCLIEnabled() {
				return result, writeMockReport(cliName, req, mockSummary)
			}
			return result, fmt.Errorf("failed to start %s: %w (CLI not installed and mock fallback disabled by config)", cliName, err)
		}
		return result, fmt.Errorf("failed to start %s: %w", cliName, err)
	}

	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		runErr := cmd.Wait()
		done <- runErr
		close(finished)
	}()

	idleDone := make(chan struct{})
	if idleTimeout > 0 {
		go func() {
			timer := time.NewTimer(idleTimeout)
			defer timer.Stop()
			for {
				select {
				case <-finished:
					return
				case <-ctxRun.Done():
					return
				case <-timer.C:
					idleFor := time.Since(time.Unix(0, lastActivityNano.Load()))
					if idleFor >= idleTimeout {
						close(idleDone)
						return
					}
					timer.Reset(idleTimeout - idleFor)
				}
			}
		}()
	}

	var runErr error
	terminated := false
	idleTerminated := false
	// Prefer an already-buffered process result over a simultaneously ready
	// watchdog/context event. This avoids classifying a just-finished command
	// as idle merely because both select cases became ready in the same tick.
	select {
	case runErr = <-done:
	default:
		select {
		case runErr = <-done:
		case <-idleDone:
			idleTerminated = true
		case <-ctxRun.Done():
			terminated = true
		}
	}

	if terminated || idleTerminated {
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			switch {
			case idleTerminated:
				log.Printf("[%s] Idle timeout reached after %v without stdout/stderr activity, killing process group %d\n", cliName, idleTimeout, pgid)
			case ctxRun.Err() == context.Canceled:
				log.Printf("[%s] Parent context cancelled, killing process group %d\n", cliName, pgid)
			default:
				log.Printf("[%s] Timeout reached, killing process group %d\n", cliName, pgid)
			}
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				log.Printf("[%s] Failed to kill process group: %v\n", cliName, killErr)
			}
		}
		select {
		case runErr = <-done:
		case <-time.After(killReapTimeout):
			if cmd.Process != nil {
				runErr = fmt.Errorf("process group %d did not exit within %v after SIGKILL", cmd.Process.Pid, killReapTimeout)
			} else {
				runErr = fmt.Errorf("process did not exit within %v after SIGKILL", killReapTimeout)
			}
			log.Printf("[%s] WARNING: %v\n", cliName, runErr)
		}
	}

	if req.Observability != nil {
		req.Observability.FlushConsole()
	}
	result.SessionID = extractOpenCodeSessionID(cliOutputPath)
	if result.SessionID == "" {
		result.SessionID = extractCodexSessionID(stderrBuf.String())
	}
	if filepath.Base(cliName) == "codex" {
		result.TokenUsage = extractCodexTokenUsage(stderrBuf.String())
	}
	result.ExitCode = exitCodeFromWaitError(runErr)

	if terminated {
		if ctxRun.Err() == context.Canceled {
			appendMetaError(cliName, metaFile, "AI execution cancelled by parent context")
			result.TerminationReason = "parent_canceled"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassCanceled, result.TerminationReason, runErr)
			return result, NewClassifiedError(ErrorClassCanceled, "AI execution cancelled")
		}
		if parentCtx.Err() == context.DeadlineExceeded {
			appendMetaError(cliName, metaFile, "AI execution stopped by parent deadline")
			result.TerminationReason = "parent_deadline"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassTimeout, result.TerminationReason, runErr)
			return result, NewClassifiedError(ErrorClassTimeout, "AI execution stopped by parent deadline")
		}
		appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution timed out after %v", timeout))
		result.TerminationReason = "attempt_timeout"
		applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassTimeout, result.TerminationReason, runErr)
		return result, NewClassifiedError(ErrorClassTimeout, "AI execution timed out after %v", timeout)
	}
	if idleTerminated {
		appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution idle timed out after %v without stdout/stderr activity", idleTimeout))
		idleErr := NewClassifiedError(
			ErrorClassIdleTimeout,
			"AI execution idle timed out after %v without stdout/stderr activity", idleTimeout,
		)
		result.TerminationReason = "idle_timeout"
		applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassIdleTimeout, result.TerminationReason, runErr)
		return result, idleErr
	}

	if err := runErr; err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			if parentCtx.Err() == context.DeadlineExceeded {
				appendMetaError(cliName, metaFile, "AI execution stopped by parent deadline")
				result.TerminationReason = "parent_deadline"
				applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassTimeout, result.TerminationReason, runErr)
				return result, NewClassifiedError(ErrorClassTimeout, "AI execution stopped by parent deadline")
			}
			appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution timed out after %v", timeout))
			result.TerminationReason = "attempt_timeout"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassTimeout, result.TerminationReason, runErr)
			return result, NewClassifiedError(ErrorClassTimeout, "AI execution timed out after %v", timeout)
		}
		if ctxRun.Err() == context.Canceled {
			appendMetaError(cliName, metaFile, "AI execution cancelled by parent context")
			result.TerminationReason = "parent_canceled"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassCanceled, result.TerminationReason, runErr)
			return result, NewClassifiedError(ErrorClassCanceled, "AI execution cancelled")
		}

		rawErrMsg := strings.TrimSpace(stderrBuf.String())
		if rawErrMsg == "" {
			if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
				rawErrMsg = strings.TrimSpace(string(content))
			}
		}
		if rawErrMsg == "" {
			rawErrMsg = err.Error()
		}
		exitCode := 0
		if result.ExitCode != nil {
			exitCode = *result.ExitCode
		}
		errMsg := compactErrorMessage(fmt.Sprintf("AI execution failed: %s exit code %d", cliName, exitCode))

		appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution failed: %s", errMsg))
		if blocked, reason := isSafetyFilterBlocked(rawErrMsg); blocked {
			classifiedErr := NewClassifiedError(
				ErrorClassContentFiltered,
				"AI execution failed: content filter triggered (%s)", truncateString(reason, 500),
			)
			result.TerminationReason = "cli_failure"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassContentFiltered, result.TerminationReason, runErr)
			return result, classifiedErr
		}
		if isAuthOrModelUnavailableError(strings.ToLower(rawErrMsg)) {
			classifiedErr := NewClassifiedError(ErrorClassAuth, "%s", errMsg)
			result.TerminationReason = "cli_failure"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassAuth, result.TerminationReason, runErr)
			return result, classifiedErr
		}
		classifiedErr := NewClassifiedError(ErrorClassUnknown, "%s", errMsg)
		result.TerminationReason = "cli_failure"
		applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassUnknown, result.TerminationReason, runErr)
		return result, classifiedErr
	}

	if err := metaFile.Sync(); err != nil {
		log.Printf("[%s] Failed to sync metaFile: %v\n", cliName, err)
	}

	// 1. 检查 CLI 标准输出中是否包含模型内容安全过滤器拦截
	if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
		trimmed := strings.TrimSpace(string(content))
		if blocked, reason := isSafetyFilterBlocked(trimmed); blocked {
			appendMetaError(cliName, metaFile, fmt.Sprintf("AI content safety filter triggered: %s", reason))
			classifiedErr := NewClassifiedError(
				ErrorClassContentFiltered,
				"AI model content filter triggered (%s): %s", cliName, truncateString(reason, 500),
			)
			result.TerminationReason = "cli_failure"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassContentFiltered, result.TerminationReason, runErr)
			return result, classifiedErr
		}
	}

	// OpenCode occasionally treats the requested JSON path as a directory and
	// writes the real payload under that directory. Recover the payload before
	// validating the output so the command does not become a false failure.
	if filepath.Base(cliName) == "opencode" && req.OutputPath != "" {
		if recErr := recoverOpenCodeDirectoryOutput(req.OutputPath); recErr != nil {
			appendMetaError(cliName, metaFile, recErr.Error())
			return result, WrapClassifiedError(ErrorClassOutputMissing, recErr, "OpenCode output recovery failed")
		}
	}

	// Read-only OpenCode agents return the final artifact in the JSON event
	// stream. Materialize the business output here so the agent itself never
	// needs filesystem-write permission.
	if filepath.Base(cliName) == "opencode" && req.OutputPath != "" {
		if _, statErr := os.Stat(req.OutputPath); os.IsNotExist(statErr) {
			if finalMessage, extractErr := extractOpenCodeFinalMessage(cliOutputPath); extractErr == nil && strings.TrimSpace(finalMessage) != "" {
				if writeErr := os.WriteFile(req.OutputPath, []byte(finalMessage), 0644); writeErr == nil {
					log.Printf("[OpenCode] Recovered final message from event stream: %s\n", req.OutputPath)
				}
			}
		}
	}

	// 2. 对于 AI 引擎，若指定了 OutputPath 但退出码为 0 时输出文件未生成或为空，杜绝假成功
	if isAIEngine(cliName) && req.OutputPath != "" {
		hasOutput := false
		if stat, err := os.Stat(req.OutputPath); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
			hasOutput = true
		} else if filepath.Base(cliName) == "codex" {
			if statMsg, errMsg := os.Stat(req.OutputPath + ".lastmsg"); errMsg == nil && statMsg.Size() > 0 {
				hasOutput = true
			}
		}

		if !hasOutput {
			sample := "(no output produced)"
			if filepath.Base(cliName) != "codex" {
				if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
					if trimmed := strings.TrimSpace(string(content)); trimmed != "" {
						sample = truncateString(trimmed, 500)
					}
				} else if trimmedStderr := strings.TrimSpace(stderrBuf.String()); trimmedStderr != "" {
					sample = truncateString(trimmedStderr, 500)
				}
			}

			if blocked, reason := isSafetyFilterBlocked(sample); blocked {
				appendMetaError(cliName, metaFile, fmt.Sprintf("AI content safety filter triggered: %s", reason))
				return result, NewClassifiedError(
					ErrorClassContentFiltered,
					"AI model content filter triggered (%s): %s", cliName, truncateString(reason, 500),
				)
			}

			errDetail := fmt.Sprintf("%s completed with exit code 0 but target output %s was not generated or empty. Output: %s",
				cliName, req.OutputPath, sample)
			compactDetail := compactErrorMessage(fmt.Sprintf("%s completed with exit code 0 but target output was not generated or empty", cliName))
			appendMetaError(cliName, metaFile, errDetail)
			if filepath.Base(cliName) == "opencode" {
				result.TerminationReason = "output_missing"
				applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassOutputMissing, result.TerminationReason, runErr)
				return result, WrapClassifiedError(
					ErrorClassOutputMissing,
					&OpenCodeOutputMissingError{
						SessionID:  result.SessionID,
						OutputPath: req.OutputPath,
						Cause:      errDetail,
					},
					"%s", errDetail,
				)
			}
			result.TerminationReason = "output_missing"
			applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassOutputMissing, result.TerminationReason, runErr)
			return result, NewClassifiedError(ErrorClassOutputMissing, "%s", compactDetail)
		}
	}

	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
		if removeErr := os.Remove(cliOutputPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[%s] Failed to remove stdout mirror %s: %v\n", cliName, cliOutputPath, removeErr)
		}
	}
	if !models.AppConfig.AI.DebugLogs {
		if removeErr := os.Remove(debugLogPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[%s] Failed to remove stale debug log %s: %v\n", cliName, debugLogPath, removeErr)
		}
	}

	result.TerminationReason = "cli_exit"
	applyCLIDiagnostics(req, cliName, result, stderrBuf.String(), ErrorClassNone, result.TerminationReason, runErr)

	return result, nil
}

// recoverOpenCodeDirectoryOutput repairs the known OpenCode output-layout issue
// where `<output>.json` is created as a directory containing the JSON payload.
func recoverOpenCodeDirectoryOutput(outputPath string) error {
	stat, err := os.Stat(outputPath)
	if err != nil || !stat.IsDir() {
		return nil
	}

	baseName := filepath.Base(outputPath)
	candidates := []string{
		filepath.Join(outputPath, baseName),
		filepath.Join(outputPath, "verdict.json"),
	}

	// If neither known layout is present, use the newest JSON payload in the
	// directory. OpenCode has emitted both `<base>.json` and `verdict.json`.
	entries, readErr := os.ReadDir(outputPath)
	if readErr != nil {
		return WrapClassifiedError(ErrorClassOutputMissing, readErr, "failed to inspect OpenCode output directory %s", outputPath)
	}
	var newestEntry string
	var newestModTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		entryPath := filepath.Join(outputPath, entry.Name())
		entryStat, statErr := os.Stat(entryPath)
		if statErr != nil || !entryStat.Mode().IsRegular() || entryStat.Size() == 0 {
			continue
		}
		if entryStat.ModTime().After(newestModTime) {
			newestEntry = entryPath
			newestModTime = entryStat.ModTime()
		}
	}
	if newestEntry != "" {
		candidates = append(candidates, newestEntry)
	}

	for _, candidate := range candidates {
		candidateStat, statErr := os.Stat(candidate)
		if statErr != nil || !candidateStat.Mode().IsRegular() || candidateStat.Size() == 0 {
			continue
		}
		if copyErr := replaceWithRegularFile(candidate, outputPath); copyErr != nil {
			return WrapClassifiedError(ErrorClassOutputMissing, copyErr, "failed to recover OpenCode output from %s", candidate)
		}
		log.Printf("[OpenCode] Recovered output file from directory layout: %s\n", candidate)
		return nil
	}

	return NewClassifiedError(
		ErrorClassOutputMissing,
		"opencode created a directory instead of output file %s and no recoverable JSON payload was found", outputPath,
	)
}

// replaceWithRegularFile stages a copy next to the destination, then replaces an
// erroneous destination directory with the recovered regular file.
func replaceWithRegularFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".recovered-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	if _, err = io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err = tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	// The destination is an invalid directory produced by OpenCode. It cannot
	// coexist with the requested regular-file path, so replace it only after
	// the recovered payload has been staged successfully.
	if err = os.RemoveAll(dstPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err = os.Rename(tmpName, dstPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// isAIEngine 判定 CLI 名称或路径是否属于受管理的 AI 交互工具
func isAIEngine(cliName string) bool {
	base := filepath.Base(cliName)
	switch base {
	case "agy", "opencode", "codex", "claude":
		return true
	default:
		return false
	}
}

// isSafetyFilterBlocked 探测大模型输出或日志中是否命中内容安全审查拦截
func isSafetyFilterBlocked(text string) (bool, string) {
	if text == "" {
		return false, ""
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "blocked by gemini's filters") ||
		strings.Contains(lower, "safety filter triggered") ||
		(strings.Contains(lower, "try rephrasing your prompt") && strings.Contains(lower, "policies")) ||
		strings.Contains(lower, "content policy violation") ||
		strings.Contains(lower, "violation of safety policy") {
		lines := strings.Split(text, "\n")
		for _, l := range lines {
			tl := strings.TrimSpace(l)
			if tl != "" {
				return true, tl
			}
		}
		return true, text
	}
	return false, ""
}
