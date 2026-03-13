package handlers

import (
	"code-shield/cron_jobs"
	"code-shield/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetSchedules(c *gin.Context) {
	var schedules []models.ScheduleConfig
	models.DB.Order("created_at desc").Find(&schedules)
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
	schedule.TargetMode = req.TargetMode
	schedule.TargetValues = req.TargetValues
	schedule.AutoNotify = req.AutoNotify
	schedule.IsActive = req.IsActive

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

func GetExecutionLogs(c *gin.Context) {
	var logs []models.TaskExecutionLog
	query := models.DB.Preload("Schedule").Preload("Repo")

	// Optional filters
	scheduleID := c.Query("schedule_id")
	if scheduleID != "" {
		query = query.Where("schedule_id = ?", scheduleID)
	}
	repoID := c.Query("repo_id")
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}

	query.Order("created_at desc").Limit(100).Find(&logs)
	c.JSON(http.StatusOK, logs)
}
