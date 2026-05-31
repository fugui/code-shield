package handlers

import (
	"code-shield/cron_jobs"
	"code-shield/models"
	"code-shield/services"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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

	// Sync cron jobs
	cron_jobs.SyncSchedules()

	c.JSON(http.StatusOK, schedule)
}

func DeleteSchedule(c *gin.Context) {
	id := c.Param("id")

	if err := models.DB.Delete(&models.ScheduleConfig{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete schedule"})
		return
	}

	// Sync cron jobs
	cron_jobs.SyncSchedules()

	c.JSON(http.StatusOK, gin.H{"message": "Schedule deleted successfully"})
}

// ExecutionLogResponse is a flattened DTO for the execution log list API.
type ExecutionLogResponse struct {
	ID           uint       `json:"id"`
	ScheduleID   *uint      `json:"schedule_id"`
	ScheduleName string     `json:"schedule_name"`
	RepoID       uint       `json:"repo_id"`
	RepoName     string     `json:"repo_name"`
	TaskTypeID   uint       `json:"task_type_id"`
	TaskTypeName string     `json:"task_type_name"`
	EngineMode   string     `json:"engine_mode"`
	TriggerType  string     `json:"trigger_type"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
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
	var logs []models.TaskExecutionLog
	query := models.DB.Preload("Schedule").Preload("Repo").Preload("TaskReport").Preload("TaskType")

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

	query.Order("created_at desc").Limit(100).Find(&logs)

	// Map to flattened DTOs
	result := make([]ExecutionLogResponse, 0, len(logs))
	for _, log := range logs {
		item := ExecutionLogResponse{
			ID:           log.ID,
			ScheduleID:   log.ScheduleID,
			RepoID:       log.RepoID,
			RepoName:     log.Repo.Name,
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
		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
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

	// If the task is running (i.e. not pending/queued), cancel it first
	if execLog.Status != models.StatusPending && execLog.Status != models.StatusQueued {
		if execLog.TaskReportID != nil {
			services.CancelRunningTask(*execLog.TaskReportID)
		}
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

	c.JSON(http.StatusOK, gin.H{"message": "已成功停止并删除该任务"})
}

// TriggerSchedule manually triggers a schedule config and queues jobs for repos immediately
func TriggerSchedule(c *gin.Context) {
	id := c.Param("id")
	
	var schedule models.ScheduleConfig
	if err := models.DB.First(&schedule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "定时策略未找到"})
		return
	}

	if err := cron_jobs.ExecuteScheduleContext(schedule.ID, "manual"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "触发策略失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "触发成功加入队列"})
}
