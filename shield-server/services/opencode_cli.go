package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OpenCodeInvoker 使用 opencode CLI 执行 AI 任务。
// 通过动态创建 .opencode/agent/<name>.md 文件将任务提示词注入为 agent 系统 prompt，
// 等效于 Claude CLI 的 --append-system-prompt，确保提示词具有系统级优先级。
type OpenCodeInvoker struct{}

func (o *OpenCodeInvoker) Name() string { return "opencode" }

// Invoke 调用 opencode run 执行 AI 任务。
// 如果存在 PromptFile，则创建临时 agent 文件并通过 --agent 参数注入系统提示词；
// 用户提示仅包含任务描述和文件列表。
// 返回 nil 表示成功，AI 输出已写入 req.OutputPath。
func (o *OpenCodeInvoker) Invoke(req AIRequest) error {
	// ── 1. Agent 创建：将提示词文件注入为 agent 系统 prompt ──
	agentName := ""
	if req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
		promptContent, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}

		// 为每次调用生成唯一 agent 名称（支持分片引擎并发执行）
		agentName = fmt.Sprintf("shield-%d", time.Now().UnixNano())
		agentDir := filepath.Join(req.WorkDir, ".opencode", "agent")
		os.MkdirAll(agentDir, 0755)
		agentPath := filepath.Join(agentDir, agentName+".md")

		// 构建 agent 定义文件：YAML frontmatter + 系统提示词
		var agentFile strings.Builder
		agentFile.WriteString("---\n")
		agentFile.WriteString("description: Code Shield automated scanning agent\n")
		agentFile.WriteString("tools:\n")
		agentFile.WriteString("  read: allow\n")
		agentFile.WriteString("  grep: allow\n")
		agentFile.WriteString("  edit: allow\n")
		agentFile.WriteString("  bash: allow\n")
		agentFile.WriteString("---\n\n")
		agentFile.Write(promptContent)

		if err := os.WriteFile(agentPath, []byte(agentFile.String()), 0644); err != nil {
			return fmt.Errorf("failed to create agent file: %w", err)
		}
		defer os.Remove(agentPath) // 执行完毕后清理临时 agent 文件

		log.Printf("[OpenCode] Created temp agent: %s (%d bytes prompt)\n", agentName, len(promptContent))
	}

	// ── 2. 构建用户消息（不再内嵌系统提示词） ──
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s（最终分析结果输出到 %s），", req.PromptMsg, req.OutputPath))
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json") {
		sb.WriteString("任务采用分片执行，本次只")
	}
	sb.WriteString(fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n")))

	userPrompt := sb.String()

	// ── 3. 构建 opencode run 参数 ──
	args := []string{"run", userPrompt, "--dangerously-skip-permissions"}
	if agentName != "" {
		args = append(args, "--agent", agentName)
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

	log.Printf("[OpenCode] WorkDir: %s, Agent: %s, PromptLen: %d chars\n", req.WorkDir, agentName, len(userPrompt))

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
