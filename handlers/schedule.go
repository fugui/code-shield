package handlers

import (
	commonAudit "code-common/backend/audit"
	commonModels "code-common/backend/models"
	"code-shield/cron_jobs"
	"code-shield/models"
	"code-shield/services"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetSchedules(c *gin.Context) {
	var schedules []models.ScheduleConfig
	models.DB.Preload("TaskType").Order("created_at desc").Find(&schedules)
	c.JSON(http.StatusOK, schedules)
}

func GetScheduleCount(c *gin.Context) {
	var count int64
	models.DB.Model(&models.ScheduleConfig{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{"count": count})
}

func CreateSchedule(c *gin.Context) {
	var req models.ScheduleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate task type exists
	if req.TaskTypeID > 0 {
		var tt models.TaskType
		if err := models.DB.First(&tt, req.TaskTypeID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "指定的任务类型不存在"})
			return
		}
	}

	if err := models.DB.Create(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create schedule"})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "create", commonModels.AuditLevelP1,
		fmt.Sprintf("创建了巡检调度策略: %s (Cron: %s)", req.Name, req.CronExpr),
		"schedule", fmt.Sprintf("%d", req.ID), req.Name,
		nil, req)

	// Sync cron jobs
	cron_jobs.SyncSchedules()

	c.JSON(http.StatusCreated, req)
}

func UpdateSchedule(c *gin.Context) {
	id := c.Param("id")
	var req models.ScheduleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var schedule models.ScheduleConfig
	if err := models.DB.First(&schedule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
		return
	}

	oldSchedule := schedule

	// Update fields
	schedule.Name = req.Name
	schedule.CronExpr = req.CronExpr
	schedule.TaskTypeID = req.TaskTypeID
	schedule.TargetMode = req.TargetMode
	schedule.TargetValues = req.TargetValues
	schedule.AutoNotify = req.AutoNotify
	schedule.IsActive = req.IsActive
	schedule.RunParams = req.RunParams

	if err := models.DB.Save(&schedule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update schedule"})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "update", commonModels.AuditLevelP1,
		fmt.Sprintf("修改了巡检调度策略: %s (Cron: %s)", schedule.Name, schedule.CronExpr),
		"schedule", fmt.Sprintf("%d", schedule.ID), schedule.Name,
		oldSchedule, schedule)

	// Sync cron jobs
	cron_jobs.SyncSchedules()

	c.JSON(http.StatusOK, schedule)
}

func DeleteSchedule(c *gin.Context) {
	id := c.Param("id")

	var schedule models.ScheduleConfig
	if err := models.DB.First(&schedule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
		return
	}

	if err := models.DB.Delete(&schedule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete schedule"})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "delete", commonModels.AuditLevelP1,
		fmt.Sprintf("删除了巡检调度策略: %s (ID: %s)", schedule.Name, id),
		"schedule", id, schedule.Name,
		schedule, nil)

	// Sync cron jobs
	cron_jobs.SyncSchedules()

	c.JSON(http.StatusOK, gin.H{"message": "Schedule deleted successfully"})
}

// ExecutionLogResponse is a flattened DTO for the execution log list API.
type ExecutionLogResponse struct {
	ID           uint                  `json:"id"`
	ScheduleID   *uint                 `json:"schedule_id"`
	ScheduleName string                `json:"schedule_name"`
	RepoID       uint                  `json:"repo_id"`
	RepoName     string                `json:"repo_name"`
	RepoURL      string                `json:"repo_url"`
	TaskTypeID   uint                  `json:"task_type_id"`
	TaskTypeName string                `json:"task_type_name"`
	EngineMode   string                `json:"engine_mode"`
	TriggerType  string                `json:"trigger_type"`
	Status       string                `json:"status"`
	ErrorMessage string                `json:"error_message"`
	StartTime    time.Time             `json:"start_time"`
	EndTime      *time.Time            `json:"end_time"`
	TaskReport   *ExecutionReportBrief `json:"task_report"`
}

// ExecutionReportBrief contains only the fields the frontend needs for the expanded row.
type ExecutionReportBrief struct {
	ID              uint   `json:"id"`
	Status          string `json:"status"`
	Score           int    `json:"score"`
	AISummary       string `json:"ai_summary"`
	TotalChunks     int    `json:"total_chunks"`
	ProcessedChunks int    `json:"processed_chunks"`
	SuccessChunks   int    `json:"success_chunks"`
}

func GetExecutionLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "25"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	var logs []models.TaskExecutionLog
	query := models.DB.Model(&models.TaskExecutionLog{}).Preload("Schedule").Preload("Repo").Preload("TaskReport").Preload("TaskType")

	// Optional filters
	scheduleID := c.Query("schedule_id")
	if scheduleID != "" {
		query = query.Where("schedule_id = ?", scheduleID)
	}
	repoID := c.Query("repo_id")
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}
	taskTypeID := c.Query("task_type_id")
	if taskTypeID != "" {
		query = query.Where("task_type_id = ?", taskTypeID)
	}

	statusGroup := c.Query("status_group")
	if statusGroup == "running" {
		query = query.Where("status IN ?", []string{"cloning", "pre_processing", "analyzing", "post_processing", "running"})
	} else if statusGroup == "pending" {
		query = query.Where("status = ?", "pending")
	} else if statusGroup == "completed" {
		query = query.Where("status IN ?", []string{"success", "failed", "skipped"})
	}

	var total int64
	query.Session(&gorm.Session{}).Count(&total)

	offset := (page - 1) * pageSize
	query.Order("status_priority ASC, id DESC").Offset(offset).Limit(pageSize).Find(&logs)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	// Map to flattened DTOs
	items := make([]ExecutionLogResponse, 0, len(logs))
	for _, log := range logs {
		item := ExecutionLogResponse{
			ID:           log.ID,
			ScheduleID:   log.ScheduleID,
			RepoID:       log.RepoID,
			RepoName:     log.Repo.Name,
			RepoURL:      log.Repo.URL,
			TaskTypeID:   log.TaskTypeID,
			TaskTypeName: log.TaskType.DisplayName,
			EngineMode:   log.TaskType.EngineMode,
			TriggerType:  log.TriggerType,
			Status:       log.Status,
			ErrorMessage: log.ErrorMessage,
			StartTime:    log.StartTime,
			EndTime:      log.EndTime,
		}
		if log.Schedule != nil {
			item.ScheduleName = log.Schedule.Name
		}
		if log.TaskReport != nil {
			item.TaskReport = &ExecutionReportBrief{
				ID:              log.TaskReport.ID,
				Status:          log.TaskReport.Status,
				Score:           log.TaskReport.Score,
				AISummary:       log.TaskReport.AISummary,
				TotalChunks:     log.TaskReport.TotalChunks,
				ProcessedChunks: log.TaskReport.ProcessedChunks,
				SuccessChunks:   log.TaskReport.SuccessChunks,
			}
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":      items,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// ClearCompletedExecutionLogs deletes all finished logs
func ClearCompletedExecutionLogs(c *gin.Context) {
	result := models.DB.
		Where("status IN ?", []string{"success", "failed", "skipped"}).
		Delete(&models.TaskExecutionLog{})

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear logs"})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "clear_logs", commonModels.AuditLevelP1,
		fmt.Sprintf("管理员清理了已完成的任务执行日志，共删除 %d 条", result.RowsAffected),
		"execution_log", "status=completed", "已完成执行记录清理",
		nil, map[string]interface{}{"deleted_count": result.RowsAffected})

	c.JSON(http.StatusOK, gin.H{"deleted": result.RowsAffected})
}

// DeletePendingExecution deletes a single pending or running execution log (and its linked TaskReport).
func DeletePendingExecution(c *gin.Context) {
	id := c.Param("id")

	var execLog models.TaskExecutionLog
	if err := models.DB.First(&execLog, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
		return
	}

	isFinished := execLog.Status == models.StatusSuccess ||
		execLog.Status == models.StatusFailed ||
		execLog.Status == models.StatusSkipped

	if isFinished {
		c.JSON(http.StatusBadRequest, gin.H{"error": "已完成（成功/失败/已跳过）的任务无法删除"})
		return
	}

	// 无条件尝试取消：消除 Worker 已领取任务但状态尚未更新为 running 的 TOCTOU 竞态窗口。
	// CancelRunningTask 在 activeTasks 中找不到 reportID 时安全返回 false，无副作用。
	if execLog.TaskReportID != nil {
		services.CancelRunningTask(*execLog.TaskReportID)
	}

	// Delete the linked TaskReport first (if any)
	if execLog.TaskReportID != nil {
		models.DB.Delete(&models.TaskReport{}, *execLog.TaskReportID)
	}

	// Delete the execution log itself
	if err := models.DB.Delete(&models.TaskExecutionLog{}, execLog.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "delete_execution", commonModels.AuditLevelP1,
		fmt.Sprintf("终止并删除了排队/运行中的执行任务 (ID: %s)", id),
		"execution_log", id, fmt.Sprintf("执行记录-%s", id),
		execLog, nil)

	c.JSON(http.StatusOK, gin.H{"message": "已成功停止并删除该任务"})
}

// BatchDeletePendingExecutions 批量删除多条排队或运行中的执行记录。
func BatchDeletePendingExecutions(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供需要删除的执行记录 ID 列表"})
		return
	}

	// 1. 查询所有待删除的执行记录
	var execLogs []models.TaskExecutionLog
	if err := models.DB.Where("id IN ?", req.IDs).Find(&execLogs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询执行记录失败: " + err.Error()})
		return
	}

	// 2. 过滤掉已完成的记录，收集可删除的 ID 和关联的 ReportID
	var deletableLogIDs []uint
	var reportIDs []uint
	skipped := 0
	for _, log := range execLogs {
		isFinished := log.Status == models.StatusSuccess ||
			log.Status == models.StatusFailed ||
			log.Status == models.StatusSkipped
		if isFinished {
			skipped++
			continue
		}
		deletableLogIDs = append(deletableLogIDs, log.ID)

		// 无条件尝试取消运行中的任务
		if log.TaskReportID != nil {
			services.CancelRunningTask(*log.TaskReportID)
			reportIDs = append(reportIDs, *log.TaskReportID)
		}
	}

	if len(deletableLogIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "没有可删除的执行记录", "deleted": 0, "skipped": skipped})
		return
	}

	// 3. 批量删除关联的 TaskReport 和 ExecutionLog
	if len(reportIDs) > 0 {
		models.DB.Where("id IN ?", reportIDs).Delete(&models.TaskReport{})
	}
	if err := models.DB.Where("id IN ?", deletableLogIDs).Delete(&models.TaskExecutionLog{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量删除失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "batch_delete_executions", commonModels.AuditLevelP1,
		fmt.Sprintf("批量停止并删除了 %d 条执行记录", len(deletableLogIDs)),
		"execution_log", fmt.Sprintf("count=%d", len(deletableLogIDs)), "批量执行记录删除",
		deletableLogIDs, nil)

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已成功删除 %d 条执行记录", len(deletableLogIDs)),
		"deleted": len(deletableLogIDs),
		"skipped": skipped,
	})
}

// TriggerSchedule manually triggers a schedule config and queues jobs for repos immediately
func TriggerSchedule(c *gin.Context) {
	id := c.Param("id")

	var schedule models.ScheduleConfig
	if err := models.DB.First(&schedule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "定时策略未找到"})
		return
	}

	var opID *uint
	opName := "管理员手动触发"
	clientIP := c.ClientIP()
	if userIDVal, exists := c.Get("userID"); exists {
		if uid, ok := userIDVal.(uint); ok && uid > 0 {
			opID = &uid
			var user models.User
			if err := models.DB.First(&user, uid).Error; err == nil {
				opName = user.Name
				if opName == "" {
					opName = user.Email
				}
			}
		}
	}
	if opName == "管理员手动触发" {
		if name, exists := c.Get("username"); exists {
			opName = fmt.Sprintf("%v", name)
		}
	}

	if err := cron_jobs.ExecuteScheduleContextWithOperator(schedule.ID, "manual", opID, opName, clientIP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "触发策略失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(c, "schedule", "trigger", commonModels.AuditLevelP1,
		fmt.Sprintf("管理员手动触发了巡检调度策略: %s (ID: %d)", schedule.Name, schedule.ID),
		"schedule", fmt.Sprintf("%d", schedule.ID), schedule.Name,
		nil, schedule)

	c.JSON(http.StatusOK, gin.H{"message": "触发成功加入队列"})
}
