package main

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultTemplate = `您好，

我们根据您管理的代码仓最新的代码提交信息，进行了 ${TASK.DISPLAYNAME}，总结如下：

${BODY}

请您基于以上信息及附件，进行分析和处理。

谢谢。`

// getTemplatePath returns the path to template.md located next to the executable
func getTemplatePath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "template.md"
	}
	return filepath.Join(filepath.Dir(exePath), "template.md")
}

// LoadTemplate reads the template from disk, or creates it with a default if it doesn't exist
func LoadTemplate() string {
	path := getTemplatePath()
	content, err := os.ReadFile(path)
	if err != nil {
		// Create default template
		os.WriteFile(path, []byte(defaultTemplate), 0644)
		return defaultTemplate
	}
	return string(content)
}

// SaveTemplate saves the current template content to disk
func SaveTemplate(content string) error {
	path := getTemplatePath()
	return os.WriteFile(path, []byte(content), 0644)
}

// RenderTemplate replaces standard placeholders with actual payload data
func RenderTemplate(templateStr string, payload NotifyPayload, summaryText string) string {
	result := templateStr
	result = strings.ReplaceAll(result, "${TASK.NAME}", payload.TaskType)
	result = strings.ReplaceAll(result, "${TASK.DISPLAYNAME}", payload.TaskDisplayName)
	// fallback if display name wasn't provided but they used the placeholder
	if payload.TaskDisplayName == "" && strings.Contains(result, "${TASK.DISPLAYNAME}") {
	    // just fallback to task_type if empty
		result = strings.ReplaceAll(result, "${TASK.DISPLAYNAME}", payload.TaskType) 
	}
	
	result = strings.ReplaceAll(result, "${REPO.NAME}", payload.RepoName)
	result = strings.ReplaceAll(result, "${BRANCH}", payload.Branch)
	result = strings.ReplaceAll(result, "${BODY}", summaryText)
	return result
}
