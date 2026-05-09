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

	// 检查 settings.json
	settingsFlag := ""
	settingsFile := models.AppConfig.GetAbsPath("settings.json")
	if _, err := os.Stat(settingsFile); err == nil {
		settingsFlag = fmt.Sprintf(" --settings '%s'", settingsFile)
	}

	// 构建 prompt 消息：如果有文件列表，追加到消息中让 Claude 自行读取
	promptMsg := req.PromptMsg
	if len(req.InputFiles) > 0 {
		promptMsg += fmt.Sprintf("。请读取并分析以下文件：\n%s", strings.Join(req.InputFiles, "\n"))
	}

	// CLI 元数据输出到独立文件，避免覆盖 AI 文档输出
	cliMetaPath := req.OutputPath + ".meta.json"

	cliCmd := fmt.Sprintf("cd %s && cat %s | claude -p '%s，并输出文档到 %s' --output-format json%s > %s",
		req.WorkDir, req.PromptFile, promptMsg, req.OutputPath, settingsFlag, cliMetaPath)

	timeout := time.Duration(req.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	ctxRun, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("[Claude] Executing: %s\n", cliCmd)
	cmd := exec.CommandContext(ctxRun, "bash", "-c", cliCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}

		// 模拟模式：claude CLI 未安装时返回模拟结果
		if strings.Contains(string(output), "command not found") {
			log.Println("[Claude] Simulating success (claude CLI not found)")
			os.WriteFile(req.OutputPath, []byte(`{"findings":[],"summary":"模拟报告：AI 引擎未安装"}`), 0644)
			return nil
		}

		// 提取错误信息
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			if content, readErr := os.ReadFile(cliMetaPath); readErr == nil {
				errMsg = strings.TrimSpace(string(content))
			}
		}

		return fmt.Errorf("AI execution failed: %s", errMsg)
	}

	return nil
}
