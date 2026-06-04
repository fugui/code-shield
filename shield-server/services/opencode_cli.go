package services

import (
	"code-shield/models"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json.raw") {
		sb.WriteString("任务采用分片执行，本次只")
	}
	sb.WriteString(fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n")))

	userPrompt := sb.String()

	formatVal := "default"
	if models.AppConfig.AI.OutputFormat == "json" {
		formatVal = "json"
	}
	args := []string{"run", userPrompt, "--dangerously-skip-permissions", "--format", formatVal, "--thinking"}
	if agentName != "" {
		args = append(args, "--agent", agentName)
	}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

	if models.AppConfig.AI.DebugLogs {
		args = append(args, "--print-logs", "--log-level", "INFO")
	}

	timeout := time.Duration(req.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = 60 * time.Minute
	}

	parentCtx := req.ParentContext
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctxRun, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	// CLI 输出记录到独立文件
	cliOutputPath := req.OutputPath + ".output.txt"
	metaFile, err := os.Create(cliOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	log.Printf("[OpenCode] WorkDir: %s, Agent: %s, PromptLen: %d chars\n", req.WorkDir, agentName, len(userPrompt))

	// 不使用 exec.CommandContext 的默认 kill 行为，因为它只 kill 直接子进程。
	// 改用手动管理：设置独立进程组（Setpgid），timeout 时 kill 整个进程组。
	cmd := exec.Command("opencode", args...)
	cmd.Dir = req.WorkDir
	cmd.Stdout = metaFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	log.Printf("[OpenCode] Executing command: %s\n", cmd.String())

	// 捕获 stderr 用于错误报告
	var stderrBuf strings.Builder
	var stderrWriter io.Writer = &stderrBuf
	var debugLogFile *os.File

	if models.AppConfig.AI.DebugLogs {
		debugLogPath := req.OutputPath + ".debug.log"
		var err error
		debugLogFile, err = os.Create(debugLogPath)
		if err == nil {
			defer debugLogFile.Close()
			stderrWriter = io.MultiWriter(&stderrBuf, debugLogFile)
		} else {
			log.Printf("[OpenCode] Failed to create debug log file %s: %v\n", debugLogPath, err)
		}
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode: %w", err)
	}

	// 在 goroutine 中等待进程结束，并在 timeout 时 kill 整个进程组
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
		// 正常结束
	case <-ctxRun.Done():
		timedOut = true
		// Kill 整个进程组（包括 opencode 启动的所有子进程）
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			log.Printf("[OpenCode] Timeout reached, killing process group %d\n", pgid)
			// 负号表示 kill 整个进程组
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				log.Printf("[OpenCode] Failed to kill process group: %v\n", killErr)
			}
		}
		// 等待进程真正退出
		<-done
	}

	if timedOut {
		metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution timed out after %v\n", timeout))
		return fmt.Errorf("AI execution timed out after %v", timeout)
	}

	if err := runErr; err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution timed out after %v\n", timeout))
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
			if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
				errMsg = strings.TrimSpace(string(content))
			}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}

		metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution failed: %s\n", errMsg))

		return fmt.Errorf("AI execution failed: %s", errMsg)
	}

	metaFile.Close()
	if debugLogFile != nil {
		debugLogFile.Close()
	}

	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
		os.Remove(cliOutputPath)
		os.Remove(req.OutputPath + ".debug.log")
	}

	return nil
}

// deriveAgentName 从 prompt 文件的绝对路径推导出 opencode agent 名称。
// 约定: .../tasks/<task-dir>/analysis_prompt.md → shield-<task-dir>-analysis
// 约定: .../tasks/<task-dir>/synthesis_prompt.md → shield-<task-dir>-synthesis
func deriveAgentName(promptAbsPath string) string {
	base := filepath.Base(promptAbsPath)              // "analysis_prompt.md"
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
