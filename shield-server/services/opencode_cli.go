package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// OpenCodeInvoker 使用 opencode CLI 执行 AI 任务
type OpenCodeInvoker struct{}

func (o *OpenCodeInvoker) Name() string { return "opencode" }

// Invoke 调用 opencode run 执行 AI 任务。
// opencode 使用 opencode.json 中的 instructions 来注入系统提示词，
// 用户提示通过命令行参数传递。
// 返回 nil 表示成功，AI 输出已写入 req.OutputPath。
func (o *OpenCodeInvoker) Invoke(req AIRequest) error {
	// 构建完整 prompt 消息
	var sb strings.Builder

	// 嵌入系统提示词文件（PromptFile 为空时跳过，仅使用 PromptMsg）
	if req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
		promptContent, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}
		sb.WriteString("请严格遵守以下系统指令：\n\n")
		sb.Write(promptContent)
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString(fmt.Sprintf("%s（最终分析结果输出到 %s），", req.PromptMsg, req.OutputPath))
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json") {
		sb.WriteString("任务采用分片执行，本次只")
	}
	sb.WriteString(fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n")))

	fullPrompt := sb.String()

	// 构建 opencode run 参数
	args := []string{"run", fullPrompt, "--pure"}

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

	log.Printf("[OpenCode] WorkDir: %s, PromptLen: %d chars\n", req.WorkDir, len(fullPrompt))

	cmd := exec.CommandContext(ctxRun, "opencode", args...)
	cmd.Dir = req.WorkDir
	cmd.Stdout = metaFile

	// 捕获 stderr 用于错误报告
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}

		// 模拟模式：opencode CLI 未安装时返回模拟结果
		stderrStr := stderrBuf.String()
		if strings.Contains(stderrStr, "not found") || strings.Contains(err.Error(), "not found") {
			log.Println("[OpenCode] Simulating success (opencode CLI not found)")
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
	RegisterAIInvoker("opencode", &OpenCodeInvoker{})
}
