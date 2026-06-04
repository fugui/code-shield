package services

import (
	"code-shield/models"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
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
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json.raw") {
		promptMsg += fmt.Sprintf("任务采用分片执行，本次只")
	}
	promptMsg += fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n"))

	formatVal := "text"
	if models.AppConfig.AI.OutputFormat == "json" {
		formatVal = "stream-json"
	}
	args := []string{"-p", promptMsg, "--output-format", formatVal, "--disable-slash-commands"}

	if req.ModelName != "" {
		args = append(args, "--model", req.ModelName)
	}

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

	log.Printf("[Claude] WorkDir: %s, Args: %v\n", req.WorkDir, args)

	// 不使用 exec.CommandContext 的默认 kill 行为，因为它只 kill 直接子进程。
	// 改用手动管理：设置独立进程组（Setpgid），timeout 时 kill 整个进程组。
	cmd := exec.Command("claude", args...)
	cmd.Dir = req.WorkDir
	cmd.Stdout = metaFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

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
			log.Printf("[Claude] Failed to create debug log file %s: %v\n", debugLogPath, err)
		}
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
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
		// Kill 整个进程组（包括 claude 启动的所有子进程）
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			log.Printf("[Claude] Timeout reached, killing process group %d\n", pgid)
			// 负号表示 kill 整个进程组
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				log.Printf("[Claude] Failed to kill process group: %v\n", killErr)
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

func init() {
	RegisterAIInvoker("claude", &ClaudeInvoker{})
}
