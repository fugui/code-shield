package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// GetTaskTypes returns all task types
func GetTaskTypes(c *gin.Context) {
	var taskTypes []models.TaskType
	query := models.DB.Order("id asc")

	activeOnly := c.Query("active_only")
	if activeOnly == "true" {
		query = query.Where("is_active = ?", true)
	}

	query.Find(&taskTypes)
	c.JSON(http.StatusOK, taskTypes)
}

// GetTaskType returns a single task type
func GetTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}
	c.JSON(http.StatusOK, taskType)
}

// CreateTaskType creates a new task type with auto-generated default files
func CreateTaskType(c *gin.Context) {
	var req models.TaskType
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.IsBuiltin = false

	if req.NotifyTemplate == "" {
		req.NotifyTemplate = "【Code-Shield】{{.RepoName}} {{.TaskDisplayName}}报告"
	}

	if err := models.DB.Create(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task type"})
		return
	}

	// Create default files on disk using conventional paths
	absTaskDir := models.AppConfig.GetAbsPath(req.TaskDir())
	os.MkdirAll(absTaskDir, 0755)

	defaultAnalysisPrompt := "# " + req.DisplayName + " 分析指令\n\n请对当前代码仓库执行" + req.DisplayName + "任务。\n\n## 要求\n\n1. 仔细检查代码\n2. 输出结构化 JSON 分析结果\n\n## 输出格式\n\n请严格按照以下 JSON 格式输出：\n\n```json\n{\n  \"findings\": [\n    {\n      \"severity\": \"严重程度\",\n      \"category\": \"问题分类\",\n      \"file_path\": \"文件路径\",\n      \"line_number\": 0,\n      \"code_snippet\": \"相关代码片段\",\n      \"title\": \"问题标题\",\n      \"detail\": \"详细说明\",\n      \"suggestion\": \"修复建议\"\n    }\n  ],\n  \"summary\": \"整体评估摘要\"\n}\n```\n"
	defaultSynthesisPrompt := "# " + req.DisplayName + " 综合报告生成指令\n\n你将收到一份 JSON 格式的分析发现清单。请基于这些发现，生成一份面向技术管理者的 Markdown 综合报告。\n\n## 输出格式\n\n请输出 Markdown 文档，包含以下部分：\n\n1. 检视结果概要\n2. 检视摘要（300字）\n3. 发现的问题（逐条列出）\n4. 总结建议\n"
	defaultPrecondition := `#!/bin/bash
# 前置检查脚本
# exit 0 = 继续执行, exit 1 = 跳过, exit 2 = 失败
REPO_DIR="$1"
# 检查 7 天内是否有新提交
RECENT=$(git -C "$REPO_DIR" log --since="7 days ago" --oneline 2>/dev/null | head -1)
if [ -z "$RECENT" ]; then
    echo "最近 7 天无新提交"
    exit 1
fi
exit 0
`
	defaultPostprocess := `#!/usr/bin/env node
const fs = require('fs');
const reportPath = process.argv[2];

if (!reportPath || !fs.existsSync(reportPath)) {
    console.log(JSON.stringify({ score: 0, summary: "报告文件未找到", metrics: {} }));
    process.exit(0);
}

const content = fs.readFileSync(reportPath, 'utf8');
// TODO: 根据 AI 报告内容解析分数和指标
const result = {
    score: 0,
    summary: "待完善后置分析逻辑",
    metrics: {}
};
process.stdout.write(JSON.stringify(result));
`

	os.WriteFile(models.AppConfig.GetAbsPath(req.AnalysisPromptFile()), []byte(defaultAnalysisPrompt), 0644)
	os.WriteFile(models.AppConfig.GetAbsPath(req.SynthesisPromptFile()), []byte(defaultSynthesisPrompt), 0644)
	os.WriteFile(models.AppConfig.GetAbsPath(req.PreconditionScript()), []byte(defaultPrecondition), 0755)
	os.WriteFile(models.AppConfig.GetAbsPath(req.PostprocessScript()), []byte(defaultPostprocess), 0755)

	// 同步 opencode agent 文件
	services.SyncTaskTypeAgents(req)

	c.JSON(http.StatusCreated, req)
}

// UpdateTaskType updates an existing task type
func UpdateTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	var req struct {
		DisplayName     *string          `json:"display_name"`
		Description     *string          `json:"description"`
		EngineMode      *string          `json:"engine_mode"`
		EngineConfig    *json.RawMessage `json:"engine_config"`
		AIBackend       *string          `json:"ai_backend"`
		TargetScope     *string          `json:"target_scope"`
		NotifyTemplate  *string          `json:"notify_template"`
		NotifyThreshold *int             `json:"notify_threshold"`
		NotifyCc        *json.RawMessage `json:"notify_cc"`
		Timeout         *int             `json:"timeout"`
		IsActive        *bool            `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if req.DisplayName != nil {
		updates["display_name"] = *req.DisplayName
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.EngineMode != nil {
		updates["engine_mode"] = *req.EngineMode
	}
	if req.EngineConfig != nil {
		updates["engine_config"] = string(*req.EngineConfig)
	}
	if req.AIBackend != nil {
		updates["ai_backend"] = *req.AIBackend
	}
	if req.TargetScope != nil {
		updates["target_scope"] = *req.TargetScope
	}
	if req.NotifyTemplate != nil {
		updates["notify_template"] = *req.NotifyTemplate
	}
	if req.NotifyThreshold != nil {
		updates["notify_threshold"] = *req.NotifyThreshold
	}
	if req.NotifyCc != nil {
		updates["notify_cc"] = string(*req.NotifyCc)
	}
	if req.Timeout != nil {
		updates["timeout"] = *req.Timeout
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if err := models.DB.Model(&taskType).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task type"})
		return
	}

	models.DB.First(&taskType, id)
	c.JSON(http.StatusOK, taskType)
}

// DeleteTaskType deletes a task type (built-in types cannot be deleted)
func DeleteTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	if taskType.IsBuiltin {
		c.JSON(http.StatusForbidden, gin.H{"error": "内置任务类型不可删除"})
		return
	}

	if err := models.DB.Delete(&taskType).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task type"})
		return
	}

	// 清理关联的 opencode agent 文件
	services.RemoveTaskTypeAgents(taskType)

	c.JSON(http.StatusOK, gin.H{"message": "Task type deleted"})
}

// GetTaskTypeFiles returns the content of the 4 conventional files
func GetTaskTypeFiles(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	readFile := func(path string) string {
		absPath := models.AppConfig.GetAbsPath(path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return ""
		}
		return string(content)
	}

	c.JSON(http.StatusOK, gin.H{
		"analysis_prompt":  readFile(taskType.AnalysisPromptFile()),
		"synthesis_prompt": readFile(taskType.SynthesisPromptFile()),
		"precondition":     readFile(taskType.PreconditionScript()),
		"postprocess":      readFile(taskType.PostprocessScript()),
	})
}

// UpdateTaskTypeFile writes content to a specific file of a task type
func UpdateTaskTypeFile(c *gin.Context) {
	id := c.Param("id")
	fileType := c.Param("file_type")

	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var filePath string
	switch fileType {
	case "analysis_prompt":
		filePath = taskType.AnalysisPromptFile()
	case "synthesis_prompt":
		filePath = taskType.SynthesisPromptFile()
	case "precondition":
		filePath = taskType.PreconditionScript()
	case "postprocess":
		filePath = taskType.PostprocessScript()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type, must be: analysis_prompt, synthesis_prompt, precondition, or postprocess"})
		return
	}

	absPath := models.AppConfig.GetAbsPath(filePath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	perm := os.FileMode(0644)
	if fileType == "precondition" || fileType == "postprocess" {
		perm = 0755 // scripts need execute permission
	}

	if err := os.WriteFile(absPath, []byte(req.Content), perm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write file"})
		return
	}

	// 提示词文件变更时同步 opencode agent
	if fileType == "analysis_prompt" || fileType == "synthesis_prompt" {
		services.SyncTaskTypeAgents(taskType)
	}

	c.JSON(http.StatusOK, gin.H{"message": "文件已保存"})
}

// TriggerAllReposForTaskType triggers the task type for all repositories
func TriggerAllReposForTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	var repos []models.Repository
	if err := models.DB.Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repositories"})
		return
	}

	count := 0
	for _, repo := range repos {
		services.EnqueueTask(nil, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})
		count++
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": fmt.Sprintf("已成功触发 %d 个代码仓的 %s 任务", count, taskType.DisplayName),
	})
}
