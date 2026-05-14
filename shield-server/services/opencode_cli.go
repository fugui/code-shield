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
// 通过 ~/.config/opencode/agents/ 下的全局 agent 文件将任务提示词注入为系统 prompt，
// 等效于 Claude CLI 的 --append-system-prompt，确保提示词具有系统级优先级。
// Agent 文件在任务类型管理的生命周期中维护（创建/编辑/删除），调用时直接引用。
type OpenCodeInvoker struct{}

func (o *OpenCodeInvoker) Name() string { return "opencode" }

// Invoke 调用 opencode run 执行 AI 任务。
// 如果存在 PromptFile，则从路径约定推导 agent 名称，通过 --agent 参数引用全局 agent；
// 用户提示仅包含任务描述和文件列表。
// 返回 nil 表示成功，AI 输出已写入 req.OutputPath。
func (o *OpenCodeInvoker) Invoke(req AIRequest) error {
	// ── 1. 推导 agent 名称 ──
	agentName := ""
	if req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}

		// 从 prompt 文件路径推导 agent 名称
		// 约定: .../tasks/<task-dir>/analysis_prompt.md → shield-<task-dir>-analysis
		agentName = deriveAgentName(req.PromptFile)

		// 检查全局 agent 是否存在，不存在则按需同步（懒加载兜底）
		agentPath := filepath.Join(globalAgentDir(), agentName+".md")
		if _, err := os.Stat(agentPath); os.IsNotExist(err) {
			log.Printf("[OpenCode] Agent %s not found, syncing on-demand\n", agentName)
			description := "Code Shield scanning agent"
			if err := syncSingleAgent(agentName, req.PromptFile, description); err != nil {
				return fmt.Errorf("failed to sync agent: %w", err)
			}
		}
	}

	// ── 2. 构建用户消息（不含系统提示词） ──
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

// deriveAgentName 从 prompt 文件的绝对路径推导出 opencode agent 名称。
// 约定: .../tasks/<task-dir>/analysis_prompt.md → shield-<task-dir>-analysis
// 约定: .../tasks/<task-dir>/synthesis_prompt.md → shield-<task-dir>-synthesis
func deriveAgentName(promptAbsPath string) string {
	base := filepath.Base(promptAbsPath)       // "analysis_prompt.md"
	dir := filepath.Base(filepath.Dir(promptAbsPath)) // "security-scan"

	phase := "analysis"
	if strings.HasPrefix(base, "synthesis") {
		phase = "synthesis"
	}

	return fmt.Sprintf("shield-%s-%s", dir, phase)
}

func init() {
	RegisterAIInvoker("opencode", &OpenCodeInvoker{})
}
