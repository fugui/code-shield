package invoker

import (
	"code-shield/models"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// defaultCLITimeout AI CLI 执行默认超时（与 AIRequest.TimeoutMin 注释保持一致）
	defaultCLITimeout = 60 * time.Minute
	// killReapTimeout SIGKILL 后等待进程退出的二次兜底时限
	killReapTimeout = 10 * time.Second
	// maxErrorMessageSize 错误信息最大长度，防止吞入超大 stdout 导致日志爆炸
	maxErrorMessageSize = 8192
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

	sb.WriteString(fmt.Sprintf("%s（最终分析结果输出到 %s），", req.PromptMsg, req.OutputPath))
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json.raw") {
		sb.WriteString("任务采用分片执行，本次只")
	}
	sb.WriteString(fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n")))

	return sb.String(), nil
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

// RunCLIProcess 统一管理所有 AI CLI 的执行、超时、进程组治理与 Mock 降级。
func RunCLIProcess(cliName string, args []string, req AIRequest, mockSummary string) error {
	timeout := time.Duration(req.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = defaultCLITimeout
	}

	parentCtx := req.ParentContext
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctxRun, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	cliOutputPath := req.OutputPath + ".output.txt"
	metaFile, err := os.Create(cliOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	log.Printf("[%s] WorkDir: %s, Args: %s\n", cliName, req.WorkDir, summarizeCLIArgs(args))

	cmd := exec.Command(cliName, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = metaFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if len(req.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), req.Env)
	}

	var stderrBuf strings.Builder
	var stderrWriter io.Writer = &stderrBuf
	var debugLogFile *os.File
	debugLogPath := req.OutputPath + ".debug.log"

	if models.AppConfig.AI.DebugLogs {
		debugLogFile, err = os.Create(debugLogPath)
		if err == nil {
			defer debugLogFile.Close()
			stderrWriter = io.MultiWriter(&stderrBuf, debugLogFile)
		} else {
			log.Printf("[%s] Failed to create debug log file %s: %v\n", cliName, debugLogPath, err)
		}
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		if isExecutableNotFound(err) {
			if models.AppConfig.MockOnMissingCLIEnabled() {
				return writeMockReport(cliName, req, mockSummary)
			}
			return fmt.Errorf("failed to start %s: %w (CLI not installed and mock fallback disabled by config)", cliName, err)
		}
		return fmt.Errorf("failed to start %s: %w", cliName, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	terminated := false
	select {
	case runErr = <-done:
	case <-ctxRun.Done():
		terminated = true
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			if ctxRun.Err() == context.Canceled {
				log.Printf("[%s] Parent context cancelled, killing process group %d\n", cliName, pgid)
			} else {
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

	if terminated {
		if ctxRun.Err() == context.Canceled {
			appendMetaError(cliName, metaFile, "AI execution cancelled by parent context")
			return fmt.Errorf("AI execution cancelled")
		}
		appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution timed out after %v", timeout))
		return fmt.Errorf("AI execution timed out after %v", timeout)
	}

	if err := runErr; err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution timed out after %v", timeout))
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}
		if ctxRun.Err() == context.Canceled {
			appendMetaError(cliName, metaFile, "AI execution cancelled by parent context")
			return fmt.Errorf("AI execution cancelled")
		}

		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg == "" {
			if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
				errMsg = strings.TrimSpace(string(content))
			}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		errMsg = truncateString(errMsg, maxErrorMessageSize)

		appendMetaError(cliName, metaFile, fmt.Sprintf("AI execution failed: %s", errMsg))
		return fmt.Errorf("AI execution failed: %s", errMsg)
	}

	if err := metaFile.Sync(); err != nil {
		log.Printf("[%s] Failed to sync metaFile: %v\n", cliName, err)
	}

	// 1. 检查 CLI 标准输出中是否包含模型内容安全过滤器拦截
	if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
		trimmed := strings.TrimSpace(string(content))
		if blocked, reason := isSafetyFilterBlocked(trimmed); blocked {
			appendMetaError(cliName, metaFile, fmt.Sprintf("AI content safety filter triggered: %s", reason))
			return fmt.Errorf("AI model content filter triggered (%s): %s", cliName, truncateString(reason, 500))
		}
	}

	// 2. 对于 AI 引擎，若指定了 OutputPath 但退出码为 0 时输出文件未生成或为空，杜绝假成功
	if isAIEngine(cliName) && req.OutputPath != "" {
		hasOutput := false
		if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
			hasOutput = true
		} else if filepath.Base(cliName) == "codex" {
			if statMsg, errMsg := os.Stat(req.OutputPath + ".lastmsg"); errMsg == nil && statMsg.Size() > 0 {
				hasOutput = true
			}
		}

		if !hasOutput {
			var sample string
			if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
				sample = strings.TrimSpace(string(content))
			}
			if sample == "" {
				sample = strings.TrimSpace(stderrBuf.String())
			}
			if sample != "" {
				sample = truncateString(sample, 500)
			} else {
				sample = "(no output produced)"
			}

			if blocked, reason := isSafetyFilterBlocked(sample); blocked {
				appendMetaError(cliName, metaFile, fmt.Sprintf("AI content safety filter triggered: %s", reason))
				return fmt.Errorf("AI model content filter triggered (%s): %s", cliName, truncateString(reason, 500))
			}

			errDetail := fmt.Sprintf("%s completed with exit code 0 but target output %s was not generated or empty. Output: %s",
				cliName, req.OutputPath, sample)
			appendMetaError(cliName, metaFile, errDetail)
			return errors.New(errDetail)
		}
	}

	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
		if removeErr := os.Remove(cliOutputPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[%s] Failed to remove stdout mirror %s: %v\n", cliName, cliOutputPath, removeErr)
		}
	}
	if !models.AppConfig.AI.DebugLogs {
		if removeErr := os.Remove(debugLogPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[%s] Failed to remove stale debug log %s: %v\n", cliName, debugLogPath, removeErr)
		}
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

