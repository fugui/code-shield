package invoker

import (
	"code-shield/models"
	"fmt"
	"os"
	"path/filepath"
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

	args, err := o.buildArgs(req)
	if err != nil {
		return err
	}

	tempDataDir, cleanup, err := prepareIsolatedDataDir()
	if err != nil {
		return err
	}
	defer cleanup()

	req.Env = append(req.Env, fmt.Sprintf("XDG_DATA_HOME=%s", tempDataDir))

	return RunCLIProcess("opencode", args, req, "模拟报告：OpenCode AI 引擎未安装")
}

func init() {
	RegisterAIInvoker("opencode", &OpenCodeInvoker{})
}
