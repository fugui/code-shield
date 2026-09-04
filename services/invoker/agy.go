package invoker

import (
	"code-shield/models"
	"fmt"
)

// AgyInvoker 使用 Antigravity CLI (agy) 执行 AI 任务
type AgyInvoker struct{}

func (a *AgyInvoker) Name() string { return "agy" }

// buildArgs 构建 agy 命令行参数（纯函数）
func (a *AgyInvoker) buildArgs(req AIRequest) ([]string, error) {
	userPrompt, err := BuildPromptPayload(req, true)
	if err != nil {
		return nil, err
	}

	formatVal := "text"
	if models.AppConfig.AI.OutputFormat == "json" {
		formatVal = "json"
	}

	timeoutMin := req.TimeoutMin
	if timeoutMin <= 0 {
		timeoutMin = 60
	}

	args := []string{
		"-p", userPrompt,
		"--dangerously-skip-permissions",
		"--disable-slash-commands",
		"--output-format", formatVal,
		"--print-timeout", fmt.Sprintf("%dm", timeoutMin),
	}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

	return args, nil
}

// Invoke 调用 agy -p 执行 AI 任务
func (a *AgyInvoker) Invoke(req AIRequest) error {
	args, err := a.buildArgs(req)
	if err != nil {
		return err
	}

	return RunCLIProcess("agy", args, req, "模拟报告：Antigravity (agy) AI 引擎未安装")
}

func init() {
	RegisterAIInvoker("agy", &AgyInvoker{})
}
