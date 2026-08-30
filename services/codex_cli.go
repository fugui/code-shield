package services

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// CodexInvoker 使用 codex CLI 执行 AI 任务
type CodexInvoker struct{}

func (c *CodexInvoker) Name() string { return "codex" }

// buildArgs 构建 codex 命令行参数（纯函数）
// 输出捕获使用 --output-last-message：由 CLI 直接落盘模型最终消息到独立文件，
// 不依赖模型在默认 read-only 沙箱下写入 OutputPath（codex 默认 sandbox=read-only、approval=never）。
// 注意：不使用 --json，因为其输出为 CLI 事件 JSONL（thread.started / item.completed...），并非模型结果。
func (c *CodexInvoker) buildArgs(req AIRequest) ([]string, error) {
	userPrompt, err := BuildPromptPayload(req, true)
	if err != nil {
		return nil, err
	}

	args := []string{"exec", "--skip-git-repo-check", "--color", "never"}

	if req.ModelName != "" {
		args = append(args, "-m", req.ModelName)
	}

	// 模型最终消息由 CLI 捕获到独立文件，运行时再决定使用模型写盘结果还是该捕获结果
	args = append(args, "--output-last-message", req.OutputPath+".lastmsg")

	args = append(args, userPrompt)

	return args, nil
}

// Invoke 调用 codex exec 执行 AI 任务
func (c *CodexInvoker) Invoke(req AIRequest) error {
	args, err := c.buildArgs(req)
	if err != nil {
		return err
	}

	if err := RunCLIProcess("codex", args, req, "模拟报告：Codex AI 引擎未安装"); err != nil {
		return err
	}

	return finalizeCodexOutput(req)
}

// finalizeCodexOutput 确保 req.OutputPath 有内容：
// 1. 若模型已自行写盘（非只读沙箱），保留模型产物并清理捕获文件；
// 2. 否则回退使用 --output-last-message 捕获的最终消息（或 stdout 镜像）。
func finalizeCodexOutput(req AIRequest) error {
	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
		removeQuietly(req.OutputPath + ".lastmsg")
		return nil
	}

	lastMsgPath := req.OutputPath + ".lastmsg"
	content, err := os.ReadFile(lastMsgPath)
	if err != nil {
		// 最后消息文件不存在时回退 stdout 镜像
		content, err = os.ReadFile(req.OutputPath + ".output.txt")
		if err != nil {
			return fmt.Errorf("codex produced no output: %s", req.OutputPath)
		}
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return fmt.Errorf("codex produced empty output: %s", req.OutputPath)
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
