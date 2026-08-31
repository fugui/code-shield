package handlers

import (
	"code-shield/models"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetTaskDebateLogsHandler 获取任务报告关联的多智能体辩论轨迹列表
func GetTaskDebateLogsHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskReportID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	if models.DB == nil {
		c.JSON(http.StatusOK, gin.H{"items": []models.TaskDebateLog{}, "total": 0})
		return
	}

	var logs []models.TaskDebateLog
	err = models.DB.Where("task_report_id = ?", taskReportID).Order("id asc").Find(&logs).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": logs,
		"total": len(logs),
	})
}
