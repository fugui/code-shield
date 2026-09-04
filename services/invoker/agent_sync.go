package invoker

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

// legacyAgentNamePattern 匹配旧版按任务类型生成的 shield-<task>-<phase>.md 文件
var legacyAgentNamePattern = regexp.MustCompile(`^shield-.+-(analysis|synthesis)\.md$`)

// EnsureBaseAgent 确保全局 ~/.config/opencode/agents/shield-base-scanner.md 存在且内容最新
// 注意：该 Agent 配置了 bash: true，配合 --auto 跳过权限确认后并非严格只读；
// 对不可信代码仓库进行扫描时，必须在容器/低权限用户等进程级隔离环境中运行本服务。
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

// CleanupLegacyTaskAgents 清理旧版按任务类型生成的 shield-<task>-<phase>.md 遗留文件。
// 新架构只使用单一基座 Agent（shield-base-scanner），旧文件已无引用，避免磁盘残留与混淆。
func CleanupLegacyTaskAgents() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	agentDir := filepath.Join(home, ".config", "opencode", "agents")
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return // 目录不存在或不可读，无需清理
	}

	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if name == BaseScannerAgentName+".md" || !legacyAgentNamePattern.MatchString(name) {
			continue
		}
		path := filepath.Join(agentDir, name)
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("[OpenCode] Warning: failed to remove legacy agent file %s: %v\n", path, removeErr)
			continue
		}
		log.Printf("[OpenCode] Removed legacy task agent file: %s\n", name)
		removed++
	}
	if removed > 0 {
		log.Printf("[OpenCode] Cleanup complete: removed %d legacy task agent file(s)\n", removed)
	}
}
