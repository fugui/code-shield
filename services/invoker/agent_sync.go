package invoker

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const BaseScannerAgentName = "shield-base-scanner"

const BaseScannerAgentContent = `---
description: Code Shield Unified Scanning Engine
mode: primary
permission:
  "*": deny
  read:
    "*": allow
    "*.env": deny
    "*.env.*": deny
    "*.env.example": allow
  grep: allow
  glob: allow
  list: allow
  edit: deny
  bash: deny
  shell: deny
  webfetch: deny
  websearch: deny
  subagent: deny
  skill: deny
  browser: deny
  question: deny
  external_directory: deny
---

你是一个顶级的代码质量与安全审计专家。请严格遵循任务中下发的规则与输出契约进行分析与报告生成。
`

// EnsureBaseAgent 确保全局 shield-base-scanner.md 存在且内容最新
// 注意：该 Agent 采用默认拒绝矩阵，仅开放只读检索工具，禁止 Bash/Shell、编辑、联网与越界访问。
func EnsureBaseAgent() error {
	configBase := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configBase == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/tmp"
		}
		configBase = filepath.Join(home, ".config")
	}
	agentDir := filepath.Join(configBase, "opencode", "agents")
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
