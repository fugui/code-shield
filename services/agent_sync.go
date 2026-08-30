package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const BaseScannerAgentName = "shield-base-scanner"

const BaseScannerAgentContent = `---
description: Code Shield Unified Scanning Engine
tools:
  read: true
  grep: true
  edit: false
  bash: true
---

你是一个顶级的代码质量与安全审计专家。请严格遵循任务中下发的规则与输出契约进行分析与报告生成。
`

// EnsureBaseAgent 确保全局 ~/.config/opencode/agents/shield-base-scanner.md 存在且内容最新
func EnsureBaseAgent() error {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	agentDir := filepath.Join(home, ".config", "opencode", "agents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create opencode agents dir: %w", err)
	}

	agentPath := filepath.Join(agentDir, BaseScannerAgentName+".md")
	// 内容比对检测：若文件存在且内容完全一致，则无需重复 I/O
	if existing, err := os.ReadFile(agentPath); err == nil && string(existing) == BaseScannerAgentContent {
		return nil
	}

	if err := os.WriteFile(agentPath, []byte(BaseScannerAgentContent), 0644); err != nil {
		log.Printf("[OpenCode] Warning: failed to write base scanner agent: %v\n", err)
		return fmt.Errorf("failed to write base scanner agent: %w", err)
	}

	log.Printf("[OpenCode] Initialized/Updated base scanner agent at %s\n", agentPath)
	return nil
}
