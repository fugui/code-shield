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

	defaultAnalysisPrompt := "# " + req.DisplayName + " 分析指令\n\n" +
		"你是一个软件开发经验非常丰富的顶级技术专家与安全审计专家。请对当前代码仓进行 " + req.DisplayName + " 专项分析任务。\n\n" +
		"## 要求\n\n" +
		"1. 深入分析代码中与 " + req.DisplayName + " 相关的潜在安全漏洞与质量缺陷。\n" +
		"2. 排除测试代码，仅对业务核心代码进行分析。\n" +
		"3. 仅报告确实存在或极大概率触发缺陷的代码，拒绝虚报。\n" +
		"4. 必须精准指出问题发生的行号（使用字符串表示，支持单行如 \"42\" 或范围如 \"42-50\"），截取 3-10 行核心代码段。\n\n" +
		"## 输出格式约束\n\n" +
		"必须直接输出纯 JSON 字符串。绝对不得包含 ```json ... ``` 等 Markdown 代码块标记，并且 findings 字段必须符合规范。\n\n" +
		"```json\n" +
		"{\n" +
		"  \"findings\": [\n" +
		"    {\n" +
		"      \"severity\": \"阻塞|严重|主要|提示|建议\",\n" +
		"      \"category\": \"问题分类-具体子问题\",\n" +
		"      \"file_path\": \"src/example.cpp\",\n" +
		"      \"line_number\": \"42-45\",\n" +
		"      \"code_snippet\": \"原始核心代码片段（3-10行）\",\n" +
		"      \"title\": \"问题简述（一句话概括）\",\n" +
		"      \"detail\": \"详细描述风险触发路径与缺陷逻辑\",\n" +
		"      \"suggestion\": \"具体的代码修复建议与最佳实践\"\n" +
		"    }\n" +
		"  ],\n" +
		"  \"summary\": \"200-300字的整体评估摘要，描述主要隐患及其风险影响\"\n" +
		"}\n" +
		"```\n"
	defaultSynthesisPrompt := "# " + req.DisplayName + " 综合报告生成指令\n\n" +
		"你将收到一份 JSON 格式的分析发现清单（基于 @analysis_prompt.md 提示词进行的分析）。请基于这些发现，生成一份指导开发者进行安全修复的综合评估报告。\n\n" +
		"## 输出格式要求\n\n" +
		"请使用简体中文，以 Markdown 格式输出，内容包括\"概要\"、\"问题清单\"、\"总结与建议\"三部分组成。\n\n" +
		"### 一、概要\n" +
		"- 介绍本次审查的目的与主要方法。\n" +
		"- 问题总数： 3 个\n\n" +
		"> ⚠️ **概要部分的最后一行，格式约束（机器解析，不得违反）**： 必须严格使用 `问题总数： N 个` 的格式，关键字和标点均不得更改， N 为非负整数，无则填 `0`。\n\n" +
		"### 二、问题清单\n" +
		"- 按照严重程度从高到低对风险列表进行整理排版。\n" +
		"- 给出文件定位、风险分类、严重级别、原始代码片段以及具体可靠的修复代码方案。\n\n" +
		"### 三、总结与建议\n" +
		"- 一个简洁的问题缺陷总结和预防改进指导。\n"
	defaultPrecondition := `#!/bin/bash
# 前置检查脚本
# exit 0 = 继续执行, exit 1 = 跳过, exit 2 = 失败
REPO_DIR="$1"

if [ -z "$REPO_DIR" ] || [ ! -d "$REPO_DIR" ]; then
    echo "代码仓路径不存在或未提供"
    exit 2
fi

# 检查代码仓是否为空目录
if [ -z "$(ls -A "$REPO_DIR" 2>/dev/null)" ]; then
    echo "代码仓为空目录，无需执行扫描"
    exit 1
fi

echo "代码仓校验成功，开始执行扫描。"
exit 0
`

	os.WriteFile(models.AppConfig.GetAbsPath(req.AnalysisPromptFile()), []byte(defaultAnalysisPrompt), 0644)
	os.WriteFile(models.AppConfig.GetAbsPath(req.SynthesisPromptFile()), []byte(defaultSynthesisPrompt), 0644)
	os.WriteFile(models.AppConfig.GetAbsPath(req.PreconditionScript()), []byte(defaultPrecondition), 0755)

	// 写入 meta.json
	metaBytes, _ := json.MarshalIndent(req, "", "  ")
	os.WriteFile(filepath.Join(absTaskDir, "meta.json"), metaBytes, 0644)

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

	// 将最新元数据重写回磁盘的 meta.json
	absTaskDir := models.AppConfig.GetAbsPath(taskType.TaskDir())
	os.MkdirAll(absTaskDir, 0755)
	metaBytes, _ := json.MarshalIndent(taskType, "", "  ")
	os.WriteFile(filepath.Join(absTaskDir, "meta.json"), metaBytes, 0644)

	c.JSON(http.StatusOK, taskType)
}

// DeleteTaskType deletes a task type
func DeleteTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	if err := models.DB.Delete(&taskType).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task type"})
		return
	}

	// 物理删除磁盘文件夹，防止重启后再次被扫出装载
	absTaskDir := models.AppConfig.GetAbsPath(taskType.TaskDir())
	os.RemoveAll(absTaskDir)

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
