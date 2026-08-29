package handlers

import (
	commonAudit "code-common/backend/audit"
	"code-shield/models"
	"code-shield/services"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ensureConfig exists ensures a row with ID=1 exists in SystemConfig
func ensureConfigExists() {
	var config models.SystemConfig
	res := models.DB.First(&config, 1)
	if res.Error != nil {
		config = models.SystemConfig{ID: 1, AutoNotify: false}
		models.DB.Create(&config)
	}
}

func GetConfig(c *gin.Context) {
	ensureConfigExists()
	var config models.SystemConfig
	models.DB.First(&config, 1)

	info := services.Dispatcher.GetThrottleInfo()

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
		"throttle_mode":     info.ThrottleMode,
		"scale_expires_at":  info.ScaleExpiresAt,
		"manual_scale":      info.ManualScale,
		"is_manual":         info.IsManual,
		"is_work_hours":     info.IsWorkHours,
		"work_hours_config": info.WorkHoursConfig,
	})
}

func UpdateConfig(c *gin.Context) {
	ensureConfigExists()
	var req struct {
		AutoNotify       *bool    `json:"auto_notify"`
		ConcurrencyScale *float64 `json:"concurrency_scale"`
		DurationHours    *float64 `json:"duration_hours"`
		QueuePaused      *bool    `json:"queue_paused"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var config models.SystemConfig
	models.DB.First(&config, 1)

	oldBefore := map[string]interface{}{
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": services.Dispatcher.GetThrottleInfo().EffectiveScale,
	}

	if req.AutoNotify != nil {
		config.AutoNotify = *req.AutoNotify
		if err := models.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存系统通知配置失败: " + err.Error()})
			return
		}
	}

	if req.QueuePaused != nil {
		config.QueuePaused = *req.QueuePaused
		if err := models.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存队列调度状态失败: " + err.Error()})
			return
		}
		services.SetQueuePaused(*req.QueuePaused)
	}

	if req.ConcurrencyScale != nil {
		var dur time.Duration
		if req.DurationHours != nil && *req.DurationHours > 0 {
			dur = time.Duration(*req.DurationHours * float64(time.Hour))
		}
		services.Dispatcher.SetScale(*req.ConcurrencyScale, dur)
	}

	info := services.Dispatcher.GetThrottleInfo()

	newAfter := map[string]interface{}{
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
	}

	auditDesc := fmt.Sprintf("修改了系统限流/通知配置 (限流倍率: %.2f)", info.EffectiveScale)
	if req.QueuePaused != nil {
		if *req.QueuePaused {
			auditDesc += "；暂停了任务队列派发 (进入排空模式)"
		} else {
			auditDesc += "；恢复了任务队列正常派发"
		}
	}

	commonAudit.SetAuditContext(c, "config", "update", models.AuditLevelP1,
		auditDesc,
		"system_config", "1", "全局扫描限流配置",
		oldBefore, newAfter)

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
		"throttle_mode":     info.ThrottleMode,
		"scale_expires_at":  info.ScaleExpiresAt,
		"manual_scale":      info.ManualScale,
		"is_manual":         info.IsManual,
		"is_work_hours":     info.IsWorkHours,
		"work_hours_config": info.WorkHoursConfig,
	})
}
