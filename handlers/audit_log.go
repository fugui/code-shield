package handlers

import (
	"code-shield/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// GetAuditLogs returns a paginated list of task trigger audit logs
func GetAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	triggerType := c.Query("trigger_type")
	operator := c.Query("operator")
	taskTypeID := c.Query("task_type_id")
	search := c.Query("search")
	startTime := c.Query("start_time")
	endTime := c.Query("end_time")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 15
	}

	query := models.DB.Model(&models.TaskTriggerLog{})

	if triggerType != "" {
		if triggerType == "manual" {
			query = query.Where("trigger_type IN (?, ?)", "manual_single", "manual_batch")
		} else if triggerType == "cron" {
			query = query.Where("trigger_type IN (?, ?)", "cron_auto", "cron_manual")
		} else {
			query = query.Where("trigger_type = ?", triggerType)
		}
	}

	if taskTypeID != "" {
		query = query.Where("task_type_id = ?", taskTypeID)
	}

	if operator != "" {
		query = query.Where("operator_name LIKE ? OR client_ip LIKE ?", "%"+operator+"%", "%"+operator+"%")
	}

	if search != "" {
		query = query.Where("trigger_batch LIKE ? OR target_summary LIKE ? OR operator_name LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	if startTime != "" {
		if t, err := time.Parse("2006-01-02", startTime); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}

	if endTime != "" {
		if t, err := time.Parse("2006-01-02", endTime); err == nil {
			query = query.Where("created_at <= ?", t.Add(24*time.Hour))
		}
	}

	var total int64
	query.Count(&total)

	var items []models.TaskTriggerLog
	offset := (page - 1) * pageSize
	query.Preload("Operator").Preload("TaskType").Preload("Schedule").
		Order("created_at desc").Offset(offset).Limit(pageSize).Find(&items)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      items,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// GetAuditLogDetail returns a single audit log with linked task execution logs
func GetAuditLogDetail(c *gin.Context) {
	id := c.Param("id")
	var triggerLog models.TaskTriggerLog
	if err := models.DB.Preload("Operator").Preload("TaskType").Preload("Schedule").First(&triggerLog, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trigger audit log not found"})
		return
	}

	var execLogs []models.TaskExecutionLog
	models.DB.Preload("Repo").Preload("Repo.Department").Preload("TaskReport").
		Where("trigger_log_id = ?", triggerLog.ID).
		Order("id desc").Find(&execLogs)

	c.JSON(http.StatusOK, gin.H{
		"trigger_log":    triggerLog,
		"execution_logs": execLogs,
	})
}

// GetAuditLogStats returns dashboard statistics for trigger audit logs
func GetAuditLogStats(c *gin.Context) {
	var totalBatches int64
	var todayBatches int64
	var manualCount int64
	var cronCount int64
	var totalReposScanned int64

	models.DB.Model(&models.TaskTriggerLog{}).Count(&totalBatches)

	todayStart := time.Now().Truncate(24 * time.Hour)
	models.DB.Model(&models.TaskTriggerLog{}).Where("created_at >= ?", todayStart).Count(&todayBatches)

	models.DB.Model(&models.TaskTriggerLog{}).Where("trigger_type IN (?, ?)", "manual_single", "manual_batch").Count(&manualCount)
	models.DB.Model(&models.TaskTriggerLog{}).Where("trigger_type IN (?, ?)", "cron_auto", "cron_manual").Count(&cronCount)

	models.DB.Model(&models.TaskTriggerLog{}).Select("COALESCE(SUM(total_repos), 0)").Scan(&totalReposScanned)

	c.JSON(http.StatusOK, gin.H{
		"total_batches":       totalBatches,
		"today_batches":       todayBatches,
		"manual_count":        manualCount,
		"cron_count":          cronCount,
		"total_repos_scanned": totalReposScanned,
	})
}

// ClearAuditLogs allows deleting historical trigger audit logs
func ClearAuditLogs(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "0"))

	query := models.DB.Model(&models.TaskTriggerLog{})
	if days > 0 {
		cutoffTime := time.Now().AddDate(0, 0, -days)
		query = query.Where("created_at < ?", cutoffTime)
	}

	result := query.Delete(&models.TaskTriggerLog{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear audit logs: " + result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": result.RowsAffected,
		"message": "日志清除成功",
	})
}
