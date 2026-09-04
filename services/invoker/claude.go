package invoker

import (
	"code-shield/models"
	"fmt"
	"os"
)

// ClaudeInvoker 使用 claude CLI 执行 AI 任务
type ClaudeInvoker struct{}

func (c *ClaudeInvoker) Name() string { return "claude" }

// buildArgs 构建 claude 命令行参数
func (c *ClaudeInvoker) buildArgs(req AIRequest) ([]string, error) {
	promptMsg, err := BuildPromptPayload(req, false)
	if err != nil {
		return nil, err
	}

	formatVal := "text"
	if models.AppConfig.AI.OutputFormat == "json" {
		formatVal = "stream-json"
	}
	args := []string{"-p", promptMsg, "--output-format", formatVal, "--disable-slash-commands"}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

	// 将提示词文件作为系统提示词注入（优先级高于普通消息）
	if req.PromptFile != "" {
		promptContent, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read prompt file: %w", err)
		}
		args = append(args, "--append-system-prompt", string(promptContent))
	}

	// 检查 settings.json
	settingsFile := models.AppConfig.GetAbsPath("settings.json")
	if _, err := os.Stat(settingsFile); err == nil {
		args = append(args, "--settings", settingsFile)
	}

	return args, nil
}

// Invoke 调用 Claude CLI 执行 AI 任务。
func (c *ClaudeInvoker) Invoke(req AIRequest) error {
	// 校验 prompt 文件存在（PromptFile 为空时跳过，仅使用 PromptMsg）
	if req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
	}

	args, err := c.buildArgs(req)
	if err != nil {
		return err
	}

	return RunCLIProcess("claude", args, req, "模拟报告：AI 引擎未安装")
}

func init() {
	RegisterAIInvoker("claude", &ClaudeInvoker{})
}
