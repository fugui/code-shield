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

// BuildPromptPayload 组装通用的 Prompt 规约、分片提示及输入文件清单
// includePromptFile: 是否将 PromptFile 内容内联拼接到消息前部（OpenCode 与 Codex 设为 true）
func BuildPromptPayload(req AIRequest, includePromptFile bool) (string, error) {
	var sb strings.Builder

	if includePromptFile && req.PromptFile != "" {
		if _, err := os.Stat(req.PromptFile); os.IsNotExist(err) {
			return "", fmt.Errorf("prompt file not found: %s", req.PromptFile)
		}
		promptBytes, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt file: %w", err)
		}
		sb.WriteString(string(promptBytes))
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString(fmt.Sprintf("%s（最终分析结果输出到 %s），", req.PromptMsg, req.OutputPath))
	if len(req.InputFiles) > 1 && strings.HasSuffix(req.OutputPath, ".json.raw") {
		sb.WriteString("任务采用分片执行，本次只")
	}
	sb.WriteString(fmt.Sprintf("基于以下文件内容进行分析：\n%s\n", strings.Join(req.InputFiles, "\n")))

	return sb.String(), nil
}

// RunCLIProcess 统一管理所有 AI CLI 的执行、超时、进程组治理与 Mock 降级
func RunCLIProcess(cliName string, args []string, req AIRequest, mockSummary string) error {
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

	cliOutputPath := req.OutputPath + ".output.txt"
	metaFile, err := os.Create(cliOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	log.Printf("[%s] WorkDir: %s, Args: %v\n", cliName, req.WorkDir, args)

	cmd := exec.Command(cliName, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdout = metaFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	var stderrBuf strings.Builder
	var stderrWriter io.Writer = &stderrBuf
	var debugLogFile *os.File

	if models.AppConfig.AI.DebugLogs {
		debugLogPath := req.OutputPath + ".debug.log"
		debugLogFile, err = os.Create(debugLogPath)
		if err == nil {
			defer debugLogFile.Close()
			stderrWriter = io.MultiWriter(&stderrBuf, debugLogFile)
		} else {
			log.Printf("[%s] Failed to create debug log file %s: %v\n", cliName, debugLogPath, err)
		}
	}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		if strings.Contains(err.Error(), "not found") {
			log.Printf("[%s] Simulating success (%s CLI not found: %v)\n", cliName, cliName, err)
			mockPayload := fmt.Sprintf(`{"findings":[],"summary":"%s"}`, mockSummary)
			if writeErr := os.WriteFile(req.OutputPath, []byte(mockPayload), 0644); writeErr != nil {
				return fmt.Errorf("failed to write mock report: %w", writeErr)
			}
			return nil
		}
		return fmt.Errorf("failed to start %s: %w", cliName, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
	case <-ctxRun.Done():
		timedOut = true
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			log.Printf("[%s] Timeout reached, killing process group %d\n", cliName, pgid)
			if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
				log.Printf("[%s] Failed to kill process group: %v\n", cliName, killErr)
			}
		}
		<-done
	}

	if timedOut {
		if _, writeErr := metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution timed out after %v\n", timeout)); writeErr != nil {
			log.Printf("[%s] Failed to write timeout error to metaFile: %v\n", cliName, writeErr)
		}
		return fmt.Errorf("AI execution timed out after %v", timeout)
	}

	if err := runErr; err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			if _, writeErr := metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution timed out after %v\n", timeout)); writeErr != nil {
				log.Printf("[%s] Failed to write timeout error to metaFile: %v\n", cliName, writeErr)
			}
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}

		stderrStr := stderrBuf.String()
		if strings.Contains(stderrStr, "not found") || strings.Contains(err.Error(), "not found") {
			log.Printf("[%s] Simulating success (%s CLI not found)\n", cliName, cliName)
			mockPayload := fmt.Sprintf(`{"findings":[],"summary":"%s"}`, mockSummary)
			if writeErr := os.WriteFile(req.OutputPath, []byte(mockPayload), 0644); writeErr != nil {
				return fmt.Errorf("failed to write mock report: %w", writeErr)
			}
			return nil
		}

		errMsg := strings.TrimSpace(stderrStr)
		if errMsg == "" {
			if content, readErr := os.ReadFile(cliOutputPath); readErr == nil {
				errMsg = strings.TrimSpace(string(content))
			}
		}
		if errMsg == "" {
			errMsg = err.Error()
		}

		if _, writeErr := metaFile.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution failed: %s\n", errMsg)); writeErr != nil {
			log.Printf("[%s] Failed to write error msg to metaFile: %v\n", cliName, writeErr)
		}
		return fmt.Errorf("AI execution failed: %s", errMsg)
	}

	if err := metaFile.Sync(); err != nil {
		log.Printf("[%s] Failed to sync metaFile: %v\n", cliName, err)
	}
	if stat, err := os.Stat(req.OutputPath); err == nil && stat.Size() > 0 {
		_ = os.Remove(cliOutputPath)
		_ = os.Remove(req.OutputPath + ".debug.log")
	}

	return nil
}
