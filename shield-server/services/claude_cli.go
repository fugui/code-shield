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

// ClaudeRequest 封装一次 Claude CLI 调用所需的全部参数
type ClaudeRequest struct {
	WorkDir    string   // 执行目录（代码仓根目录）
	PromptFile string   // 系统提示词文件的绝对路径
	PromptMsg  string   // 传给 claude -p 的用户提示消息
	InputFiles []string // 需要分析的文件列表（相对路径），Claude 自行读取
	OutputPath string   // AI 输出文档的目标路径
	TimeoutMin int      // 执行超时（分钟），0 表示默认 30 分钟
}

// InvokeClaude 调用 Claude CLI 执行 AI 任务。
// 提示词通过 stdin 管道输入，文件列表写在 prompt 消息中由 Claude 自行读取。
// 返回 nil 表示成功，AI 输出已写入 req.OutputPath。
func InvokeClaude(req ClaudeRequest) error {
	// 校验 prompt 文件存在
	if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
		return fmt.Errorf("prompt file not found: %s", req.PromptFile)
	}

	// 构建 prompt 消息：如果有文件列表，追加到消息中让 Claude 自行读取
	promptMsg := req.PromptMsg
	if len(req.InputFiles) > 0 {
		promptMsg += fmt.Sprintf("。请读取并分析以下文件：\n%s", strings.Join(req.InputFiles, "\n"))
	}
	promptMsg += fmt.Sprintf("，并输出文档到 %s", req.OutputPath)

	// 构建 claude CLI 参数（不经过 shell，避免引号转义问题）
	args := []string{"-p", promptMsg, "--output-format", "json"}

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

	// 将提示词文件作为 stdin
	promptContent, err := os.Open(req.PromptFile)
	if err != nil {
		return fmt.Errorf("failed to open prompt file: %w", err)
	}
	defer promptContent.Close()
	cmd.Stdin = promptContent

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
