package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetTasks returns a paginated list of task reports (replaces GetReviews)
func GetTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	repoID := c.Query("repo_id")
	taskType := c.Query("task_type")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 15
	}

	query := models.DB.Model(&models.TaskReport{})
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}
	if taskType != "" {
		query = query.Where("task_type_id = ?", taskType)
	}

	var total int64
	query.Count(&total)

	var reports []models.TaskReport
	offset := (page - 1) * pageSize
	query.Preload("Repo").Preload("TaskType").Order("created_at desc").Offset(offset).Limit(pageSize).Find(&reports)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      reports,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// GetTaskDetails returns a single task report
func GetTaskDetails(c *gin.Context) {
	id := c.Param("id")
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetTaskReportMarkdown returns the raw markdown of the report file
func GetTaskReportMarkdown(c *gin.Context) {
	id := c.Param("id")
	var report models.TaskReport
	if err := models.DB.First(&report, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}
	if report.ReportPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report path is missing"})
		return
	}
	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read report file"})
		return
	}
	c.String(http.StatusOK, string(content))
}

// TriggerTask triggers a task for a specific repository
func TriggerTask(c *gin.Context) {
	var req struct {
		RepoID     uint `json:"repo_id" binding:"required"`
		TaskTypeID uint `json:"task_type_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var repo models.Repository
	if err := models.DB.First(&repo, req.RepoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Repo not found"})
		return
	}

	var taskType models.TaskType
	if err := models.DB.First(&taskType, req.TaskTypeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	services.EnqueueTask(nil, repo.ID, repo.URL, taskType.ID, false, "manual")

	c.JSON(http.StatusAccepted, gin.H{"message": taskType.DisplayName + " 任务已下发"})
}

// TriggerManualNotification sends a notification for a specific task report
func TriggerManualNotification(c *gin.Context) {
	reportID := c.Param("id")

	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, reportID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}

	if report.Status != "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only successful reports can be notified"})
		return
	}

	mdContent := ""
	if report.ReportPath != "" {
		contentBytes, err := os.ReadFile(report.ReportPath)
		if err == nil {
			mdContent = string(contentBytes)
		}
	}

	var specificEmail string
	if userID, exists := c.Get("userID"); exists {
		var user models.User
		if err := models.DB.First(&user, userID).Error; err == nil && !user.IsAdmin {
			var member models.Member
			models.DB.Where("id = ? OR name = ?", user.Username, user.Username).First(&member)
			if member.Email != "" {
				specificEmail = member.Email
			} else {
				specificEmail = user.Username
			}
		}
	}

	result := services.TaskResult{
		Score:   report.Score,
		Summary: report.AISummary,
	}

	services.NotifyTaskResult(report.Repo, report.TaskType, result, mdContent, specificEmail)

	c.JSON(http.StatusOK, gin.H{"message": "Notification dispatched"})
}

// GetTaskOverview returns a paginated list of repositories with their latest task statistics
func GetTaskOverview(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	teamID := c.Query("team_id")
	serviceGroup := c.Query("service_group")
	owner := c.Query("owner")
	taskTypeID := c.Query("task_type_id")
	sort := c.DefaultQuery("sort", "latest_task_time_desc")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 15
	}

	query := models.DB.Model(&models.Repository{})

	if teamID != "" {
		query = query.Where("repositories.team_id = ?", teamID)
	}
	if serviceGroup != "" {
		query = query.Where("repositories.service_group LIKE ?", "%"+serviceGroup+"%")
	}
	if owner != "" {
		query = query.Joins("LEFT JOIN members ON repositories.owner_id = members.id").
			Where("members.name LIKE ? OR repositories.owner_id LIKE ?", "%"+owner+"%", "%"+owner+"%")
	}

	var total int64
	query.Count(&total)

	// Subquery: latest task report for each repo (optionally filtered by task_type_id)
	subQuery := models.DB.Model(&models.TaskReport{}).Select("MAX(id)").Group("repo_id")
	if taskTypeID != "" {
		subQuery = subQuery.Where("task_type_id = ?", taskTypeID)
	}

	// Subquery: count reports per repo
	countSubQuery := models.DB.Model(&models.TaskReport{}).Select("repo_id, COUNT(*) as cnt").Group("repo_id")
	if taskTypeID != "" {
		countSubQuery = countSubQuery.Where("task_type_id = ?", taskTypeID)
	}

	query = query.
		Select("repositories.*, tr.id as latest_task_id, tr.status as latest_task_status, tr.created_at as latest_task_time, tr.score as latest_task_score, tr.task_type_id, COALESCE(rc.cnt, 0) as report_count").
		Joins("LEFT JOIN task_reports tr ON tr.id IN (?) AND tr.repo_id = repositories.id", subQuery).
		Joins("LEFT JOIN (?) rc ON rc.repo_id = repositories.id", countSubQuery)

	if sort == "latest_task_time_desc" {
		query = query.Order("latest_task_time DESC NULLS LAST, repositories.id DESC")
	} else if sort == "latest_task_time_asc" {
		query = query.Order("latest_task_time ASC NULLS LAST, repositories.id ASC")
	} else if sort == "status_desc" {
		query = query.Order("latest_task_status DESC NULLS LAST, repositories.id DESC")
	} else if sort == "status_asc" {
		query = query.Order("latest_task_status ASC NULLS LAST, repositories.id ASC")
	} else {
		query = query.Order("repositories.id DESC")
	}

	type ResultItem struct {
		models.Repository
		LatestTaskID     *uint
		LatestTaskStatus *string
		LatestTaskTime   *string
		LatestTaskScore  *int
		TaskTypeID       *uint
		ReportCount      int
	}

	var results []ResultItem
	offset := (page - 1) * pageSize
	query.Preload("Team").Preload("Owner").Offset(offset).Limit(pageSize).Find(&results)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	type OverviewItem struct {
		Repo             models.Repository `json:"repo"`
		LatestTaskID     uint              `json:"latest_task_id"`
		LatestTaskStatus string            `json:"latest_task_status"`
		LatestTaskTime   string            `json:"latest_task_time"`
		LatestTaskScore  int               `json:"latest_task_score"`
		TaskTypeID       uint              `json:"task_type_id"`
		ReportCount      int               `json:"report_count"`
	}

	var items []OverviewItem
	for _, res := range results {
		item := OverviewItem{
			Repo:        res.Repository,
			ReportCount: res.ReportCount,
		}

		if res.LatestTaskStatus != nil {
			if res.LatestTaskID != nil {
				item.LatestTaskID = *res.LatestTaskID
			}
			item.LatestTaskStatus = *res.LatestTaskStatus
			if res.LatestTaskTime != nil {
				t := *res.LatestTaskTime
				if len(t) > 19 {
					t = t[:19]
				}
				item.LatestTaskTime = t
			}
			if res.LatestTaskScore != nil {
				item.LatestTaskScore = *res.LatestTaskScore
			}
			if res.TaskTypeID != nil {
				item.TaskTypeID = *res.TaskTypeID
			}
		} else {
			item.LatestTaskStatus = "none"
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
