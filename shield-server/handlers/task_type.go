package handlers

import (
	"code-shield/models"
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

// CreateTaskType creates a new task type with auto-generated file paths and default files
func CreateTaskType(c *gin.Context) {
	var req models.TaskType
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.IsBuiltin = false

	// Auto-generate file paths based on task name
	taskDir := filepath.Join("tasks", req.Name)
	req.PromptFile = filepath.Join(taskDir, "prompt.md")
	req.PreconditionScript = filepath.Join(taskDir, "precondition.sh")
	req.PostprocessScript = filepath.Join(taskDir, "postprocess.sh")

	if req.NotifyTemplate == "" {
		req.NotifyTemplate = "【Code-Shield】{{.RepoName}} {{.TaskDisplayName}}报告"
	}

	if err := models.DB.Create(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task type"})
		return
	}

	// Create default files on disk
	os.MkdirAll(taskDir, 0755)

	defaultPrompt := "# " + req.DisplayName + "\n\n请对当前代码仓库执行" + req.DisplayName + "任务。\n\n## 要求\n\n1. 仔细检查代码\n2. 输出详细的分析报告\n"
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
	defaultPostprocess := `#!/bin/bash
# 后置分析脚本
# 输入: $1 = 报告文件路径
# 输出: JSON {"score": N, "summary": "...", "metrics": {...}}
REPORT="$1"
if [ ! -f "$REPORT" ]; then
    echo '{"score": 0, "summary": "报告文件未找到", "metrics": {}}'
    exit 0
fi
echo '{"score": 0, "summary": "待完善后置分析逻辑", "metrics": {}}'
`

	os.WriteFile(req.PromptFile, []byte(defaultPrompt), 0644)
	os.WriteFile(req.PreconditionScript, []byte(defaultPrecondition), 0755)
	os.WriteFile(req.PostprocessScript, []byte(defaultPostprocess), 0755)

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
		DisplayName        *string `json:"display_name"`
		Description        *string `json:"description"`
		PromptFile         *string `json:"prompt_file"`
		PreconditionScript *string `json:"precondition_script"`
		PostprocessScript  *string `json:"postprocess_script"`
		NotifyTemplate     *string `json:"notify_template"`
		NotifyThreshold    *int    `json:"notify_threshold"`
		Timeout            *int    `json:"timeout"`
		IsActive           *bool   `json:"is_active"`
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
	if req.PromptFile != nil {
		updates["prompt_file"] = *req.PromptFile
	}
	if req.PreconditionScript != nil {
		updates["precondition_script"] = *req.PreconditionScript
	}
	if req.PostprocessScript != nil {
		updates["postprocess_script"] = *req.PostprocessScript
	}
	if req.NotifyTemplate != nil {
		updates["notify_template"] = *req.NotifyTemplate
	}
	if req.NotifyThreshold != nil {
		updates["notify_threshold"] = *req.NotifyThreshold
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

	c.JSON(http.StatusOK, gin.H{"message": "Task type deleted"})
}

// GetTaskTypeFiles returns the content of prompt, precondition, and postprocess files
func GetTaskTypeFiles(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	readFile := func(path string) string {
		if path == "" {
			return ""
		}
		absPath := models.AppConfig.GetAbsPath(path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return ""
		}
		return string(content)
	}

	c.JSON(http.StatusOK, gin.H{
		"prompt":       readFile(taskType.PromptFile),
		"precondition": readFile(taskType.PreconditionScript),
		"postprocess":  readFile(taskType.PostprocessScript),
	})
}

// UpdateTaskTypeFile writes content to a specific file of a task type
func UpdateTaskTypeFile(c *gin.Context) {
	id := c.Param("id")
	fileType := c.Param("file_type") // "prompt", "precondition", "postprocess"

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
	case "prompt":
		filePath = taskType.PromptFile
	case "precondition":
		filePath = taskType.PreconditionScript
	case "postprocess":
		filePath = taskType.PostprocessScript
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type, must be: prompt, precondition, or postprocess"})
		return
	}

	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "此任务类型未配置该文件路径，请先在编辑中设置路径"})
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

	c.JSON(http.StatusOK, gin.H{"message": "文件已保存"})
}
