package services

import (
	"code-shield/models"
)

// CodexInvoker 使用 codex CLI 执行 AI 任务
type CodexInvoker struct{}

func (c *CodexInvoker) Name() string { return "codex" }

// buildArgs 构建 codex 命令行参数（纯函数）
func (c *CodexInvoker) buildArgs(req AIRequest) ([]string, error) {
	userPrompt, err := BuildPromptPayload(req, true)
	if err != nil {
		return nil, err
	}

	args := []string{"exec", "--skip-git-repo-check", "--color", "never"}

	if req.ModelName != "" {
		args = append(args, "-m", req.ModelName)
	}

	if models.AppConfig.AI.OutputFormat == "json" {
		args = append(args, "--json")
	}

	args = append(args, userPrompt)

	return args, nil
}

// Invoke 调用 codex exec 执行 AI 任务
func (c *CodexInvoker) Invoke(req AIRequest) error {
	args, err := c.buildArgs(req)
	if err != nil {
		return err
	}

	return RunCLIProcess("codex", args, req, "模拟报告：Codex AI 引擎未安装")
}

func init() {
	RegisterAIInvoker("codex", &CodexInvoker{})
}
