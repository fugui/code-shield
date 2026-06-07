package services

import (
	"code-shield/models"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// globalAgentDir 返回 opencode 全局 agent 目录路径
func globalAgentDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[AgentSync] Warning: cannot determine home dir: %v, using /tmp\n", err)
		home = "/tmp"
	}
	return filepath.Join(home, ".config", "opencode", "agents")
}

// buildAgentContent 构建 opencode agent 文件内容（YAML frontmatter + 系统提示词）
func buildAgentContent(description string, promptContent []byte) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("description: %s\n", description))
	sb.WriteString("tools:\n")
	sb.WriteString("  read: true\n")
	sb.WriteString("  grep: true\n")
	sb.WriteString("  edit: true\n")
	sb.WriteString("  bash: true\n")
	sb.WriteString("---\n\n")
	sb.Write(promptContent)
	return sb.String()
}

// syncSingleAgent 为指定的提示词文件生成/更新全局 opencode agent
func syncSingleAgent(agentName string, promptAbsPath string, description string) error {
	promptContent, err := os.ReadFile(promptAbsPath)
	if err != nil {
		return fmt.Errorf("failed to read prompt file %s: %w", promptAbsPath, err)
	}

	agentDir := globalAgentDir()
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent dir: %w", err)
	}

	agentPath := filepath.Join(agentDir, agentName+".md")
	newContent := buildAgentContent(description, promptContent)

	// 仅在内容变化时写入（避免无意义的 I/O）
	if existing, err := os.ReadFile(agentPath); err == nil && string(existing) == newContent {
		return nil
	}

	if err := os.WriteFile(agentPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write agent file %s: %w", agentPath, err)
	}

	log.Printf("[AgentSync] Synced agent: %s (%d bytes)\n", agentName, len(promptContent))
	return nil
}

// SyncTaskTypeAgents 为指定 TaskType 生成/更新全局 opencode agent 文件。
// 读取 analysis_prompt.md 和 synthesis_prompt.md，写入 ~/.config/opencode/agents/
func SyncTaskTypeAgents(taskType models.TaskType) {
	phases := []struct {
		name       string
		promptFile string
	}{
		{"analysis", taskType.AnalysisPromptFile()},
		{"synthesis", taskType.SynthesisPromptFile()},
	}

	for _, phase := range phases {
		absPrompt := models.AppConfig.GetAbsPath(phase.promptFile)
		if _, err := os.Stat(absPrompt); os.IsNotExist(err) {
			continue // 提示词文件不存在，跳过
		}

		agentName := taskType.AgentName(phase.name)
		description := fmt.Sprintf("%s - %s", taskType.DisplayName, phase.name)

		if err := syncSingleAgent(agentName, absPrompt, description); err != nil {
			log.Printf("[AgentSync] Error syncing %s: %v\n", agentName, err)
		}
	}
}

// RemoveTaskTypeAgents 删除指定 TaskType 关联的 opencode agent 文件
func RemoveTaskTypeAgents(taskType models.TaskType) {
	agentDir := globalAgentDir()
	for _, phase := range []string{"analysis", "synthesis"} {
		agentPath := filepath.Join(agentDir, taskType.AgentName(phase)+".md")
		if err := os.Remove(agentPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[AgentSync] Error removing %s: %v\n", agentPath, err)
		} else if err == nil {
			log.Printf("[AgentSync] Removed agent: %s\n", taskType.AgentName(phase))
		}
	}
}

// SyncAllAgents 遍历所有活跃 TaskType，批量同步 agent 文件。
// 在服务器启动时调用，确保 agent 文件与提示词文件一致。
// 同时清理孤立的 shield-* agent 文件（TaskType 已删除但文件残留）。
func SyncAllAgents() {
	var taskTypes []models.TaskType
	models.DB.Where("is_active = ?", true).Find(&taskTypes)

	// 同步所有活跃 TaskType 的 agent
	validAgents := make(map[string]bool)
	for _, tt := range taskTypes {
		SyncTaskTypeAgents(tt)
		validAgents[tt.AgentName("analysis")+".md"] = true
		validAgents[tt.AgentName("synthesis")+".md"] = true
	}

	// 清理孤立的 shield-* agent 文件
	agentDir := globalAgentDir()
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return // 目录不存在或不可读，跳过清理
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "shield-") {
			continue // 不是我们生成的 agent，跳过
		}
		if !validAgents[name] {
			orphanPath := filepath.Join(agentDir, name)
			os.Remove(orphanPath)
			log.Printf("[AgentSync] Cleaned orphan agent: %s\n", name)
		}
	}

	log.Printf("[AgentSync] Synced %d task types, %d agent files\n", len(taskTypes), len(validAgents))
}
