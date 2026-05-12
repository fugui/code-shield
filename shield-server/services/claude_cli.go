package services

import (
	"code-shield/models"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeInvoker 使用 claude CLI 执行 AI 任务
type ClaudeInvoker struct{}

func (c *ClaudeInvoker) Name() string { return "claude" }

// Invoke 调用 Claude CLI 执行 AI 任务。
// 提示词通过 --append-system-prompt 注入为系统级指令，文件列表写在 -p 消息中由 Claude 自行读取。
// 返回 nil 表示成功，AI 输出已写入 req.OutputPath。
func (c *ClaudeInvoker) Invoke(req AIRequest) error {
	// 校验 prompt 文件存在（PromptFile 为空时跳过，仅使用 PromptMsg）
	if req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
	}

	// 构建 prompt 消息：如果有文件列表，追加到消息中让 Claude 自行读取
	promptMsg := fmt.Sprintf("%s（最终分析结果输出到 %s），", req.PromptMsg, req.OutputPath)
	if len(req.InputFiles) > 0 && strings.HasSuffix(req.OutputPath, ".json") {
		promptMsg += fmt.Sprintf("任务采用分片执行，本次只" )
	}
	promptMsg += fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n"))

	// 构建 claude CLI 参数（不经过 shell，避免引号转义问题）
	args := []string{"-p", promptMsg, "--output-format", "json", "--disable-slash-commands"}

	// 将提示词文件作为系统提示词注入（优先级高于普通消息）
	if req.PromptFile != "" {
		promptContent, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}
		args = append(args, "--append-system-prompt", string(promptContent))
	}

	// 检查 settings.json
	settingsFile := models.AppConfig.GetAbsPath("settings.json")
	if _, err := os.Stat(settingsFile); err == nil {
		args = append(args, "--settings", settingsFile)
	}

	timeout := time.Duration(req.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	ctxRun, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// CLI 元数据输出到独立文件
	cliMetaPath := req.OutputPath + ".meta.json"
	metaFile, err := os.Create(cliMetaPath)
	if err != nil {
		return fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	log.Printf("[Claude] WorkDir: %s, Args: %v\n", req.WorkDir, args)

	cmd := exec.CommandContext(ctxRun, "claude", args...)
	cmd.Dir = req.WorkDir
	cmd.Stdout = metaFile

	// 捕获 stderr 用于错误报告
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}

		// 模拟模式：claude CLI 未安装时返回模拟结果
		stderrStr := stderrBuf.String()
		if strings.Contains(stderrStr, "not found") || strings.Contains(err.Error(), "not found") {
			log.Println("[Claude] Simulating success (claude CLI not found)")
			os.WriteFile(req.OutputPath, []byte(`{"findings":[],"summary":"模拟报告：AI 引擎未安装"}`), 0644)
			return nil
		}

		// 提取错误信息
		errMsg := strings.TrimSpace(stderrStr)
		if errMsg == "" {
			if content, readErr := os.ReadFile(cliMetaPath); readErr == nil {
				errMsg = strings.TrimSpace(string(content))
			}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}

		return fmt.Errorf("AI execution failed: %s", errMsg)
	}

	return nil
}

func init() {
	RegisterAIInvoker("claude", &ClaudeInvoker{})
}
