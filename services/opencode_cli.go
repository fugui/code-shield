package services

import (
	"code-shield/models"
	"fmt"
)

// OpenCodeInvoker 使用 opencode CLI 执行 AI 任务。
// 通过全局 shield-base-scanner 基础 Agent 承载通用权限，具体任务规约通过动态 Prompt 注入。
type OpenCodeInvoker struct{}

func (o *OpenCodeInvoker) Name() string { return "opencode" }

// buildArgs 构建 opencode 命令行参数（纯函数）
func (o *OpenCodeInvoker) buildArgs(req AIRequest) ([]string, error) {
	userPrompt, err := BuildPromptPayload(req, true)
	if err != nil {
		return nil, err
	}

	formatVal := "default"
	if models.AppConfig.AI.OutputFormat == "json" {
		formatVal = "json"
	}

	args := []string{
		"run", userPrompt,
		"--agent", BaseScannerAgentName,
		"--auto",
		"--format", formatVal,
		"--thinking",
	}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

	if models.AppConfig.AI.DebugLogs {
		args = append(args, "--print-logs", "--log-level", "INFO")
	}

	return args, nil
}

// Invoke 调用 opencode run 执行 AI 任务
func (o *OpenCodeInvoker) Invoke(req AIRequest) error {
	if err := EnsureBaseAgent(); err != nil {
		return fmt.Errorf("failed to ensure base agent: %w", err)
	}

	args, err := o.buildArgs(req)
	if err != nil {
		return err
	}

	return RunCLIProcess("opencode", args, req, "模拟报告：OpenCode AI 引擎未安装")
}

func init() {
	RegisterAIInvoker("opencode", &OpenCodeInvoker{})
}
