package invoker

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// CodexInvoker 使用 codex CLI 执行 AI 任务
type CodexInvoker struct{}

func (c *CodexInvoker) Name() string { return "codex" }

// codexInlinePromptMaxBytes 与 OpenCode 的限制保持一致。Linux 对单个 argv 字符串
// 有 MAX_ARG_STRLEN 约束，超过该阈值时 Codex CLI 支持 "-" 从 stdin 读取 Prompt。
const codexInlinePromptMaxBytes = 96 * 1024

// buildArgs 构建 codex 命令行参数，并返回需要通过 stdin 传输的 Prompt。
// 输出捕获使用 --output-last-message：由 CLI 直接落盘模型最终消息到独立文件，
// 不依赖模型在默认 read-only 沙箱下写入 OutputPath（codex 默认 sandbox=read-only、approval=never）。
// 注意：不使用 --json，因为其输出为 CLI 事件 JSONL（thread.started / item.completed...），并非模型结果。
func (c *CodexInvoker) buildArgs(req AIRequest) ([]string, string, error) {
	userPrompt, err := BuildReadOnlyPromptPayload(req, true)
	if err != nil {
		return nil, "", err
	}

	args := []string{"exec", "--skip-git-repo-check", "--color", "never", "--sandbox", "read-only"}

	if req.WorkDir != "" {
		args = append(args, "-C", req.WorkDir)
	}

	if req.ModelName != "" {
		if profile, ok := strings.CutPrefix(req.ModelName, "profile:"); ok {
			args = append(args, "--profile", profile)
		} else {
			args = append(args, "-m", req.ModelName)
		}
	}

	// 模型最终消息由 CLI 捕获到独立文件；只读沙箱下不依赖模型写盘。
	args = append(args, "--output-last-message", req.OutputPath+".lastmsg")

	promptArg := userPrompt
	stdinPayload := ""
	if len(userPrompt) > codexInlinePromptMaxBytes {
		promptArg = "-"
		stdinPayload = userPrompt
		log.Printf("[Codex] Large prompt switched to stdin transport: size=%d bytes\n", len(userPrompt))
	}
	args = append(args, promptArg)

	return args, stdinPayload, nil
}

// Invoke 调用 codex exec 执行 AI 任务
func (c *CodexInvoker) Invoke(req AIRequest) error {
	args, stdinPayload, err := c.buildArgs(req)
	if err != nil {
		return err
	}
	req.StdinPayload = stdinPayload

	processResult, err := RunCLIProcessWithResult("codex", args, req, "模拟报告：Codex AI 引擎未安装")
	if err != nil {
		return err
	}

	return finalizeCodexOutput(req, processResult)
}

// finalizeCodexOutput ensures req.OutputPath has content:
// 1. keep an artifact written by the model when a writable sandbox is used;
// 2. otherwise materialize only --output-last-message, never stdout/stderr.
func finalizeCodexOutput(req AIRequest, processResult CLIProcessResult) error {
	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
		removeQuietly(req.OutputPath + ".lastmsg")
		return nil
	}

	lastMsgPath := req.OutputPath + ".lastmsg"
	content, err := os.ReadFile(lastMsgPath)
	if err != nil {
		classifiedErr := NewClassifiedError(
			ErrorClassOutputMissing,
			"codex produced no final message",
		)
		processResult.TerminationReason = "output_missing"
		req.Observability.MergeCLIDiagnostics(func(diag *CLIDiagnostics) {
			diag.CLI = "codex"
			diag.SessionID = processResult.SessionID
			diag.ExitCode = processResult.ExitCode
			diag.TerminationReason = processResult.TerminationReason
			diag.ErrorClass = ErrorClassOutputMissing
			diag.TokenUsage = processResult.TokenUsage
		})
		return classifiedErr
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		classifiedErr := NewClassifiedError(
			ErrorClassOutputMissing,
			"codex produced empty final message",
		)
		processResult.TerminationReason = "output_missing"
		req.Observability.MergeCLIDiagnostics(func(diag *CLIDiagnostics) {
			diag.CLI = "codex"
			diag.SessionID = processResult.SessionID
			diag.ExitCode = processResult.ExitCode
			diag.TerminationReason = processResult.TerminationReason
			diag.ErrorClass = ErrorClassOutputMissing
			diag.TokenUsage = processResult.TokenUsage
		})
		return classifiedErr
	}
	if err := os.WriteFile(req.OutputPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write codex output: %w", err)
	}
	removeQuietly(lastMsgPath)
	return nil
}

// removeQuietly 尽力清理临时文件，失败仅记录日志
func removeQuietly(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[Codex] Failed to remove temp output file %s: %v\n", path, err)
	}
}

func init() {
	RegisterAIInvoker("codex", &CodexInvoker{})
}
