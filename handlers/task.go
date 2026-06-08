package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetTasks returns a paginated list of task reports (replaces GetReviews)
func GetTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	repoID := c.Query("repo_id")
	taskTypeID := c.Query("task_type_id")
	if taskTypeID == "" {
		taskTypeID = c.Query("task_type")
	}
	teamID := c.Query("team_id")
	serviceGroup := c.Query("service_group")
	owner := c.Query("owner")
	status := c.Query("status")
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 15
	}

	query := models.DB.Model(&models.TaskReport{}).
		Joins("LEFT JOIN repositories ON task_reports.repo_id = repositories.id")

	if repoID != "" {
		query = query.Where("task_reports.repo_id = ?", repoID)
	}
	if taskTypeID != "" {
		query = query.Where("task_reports.task_type_id = ?", taskTypeID)
	}
	if teamID != "" {
		query = query.Where("repositories.department_id = ?", teamID)
	}
	if serviceGroup != "" {
		query = query.Where("repositories.service_group LIKE ?", "%"+serviceGroup+"%")
	}
	if owner != "" {
		query = query.Joins("LEFT JOIN users ON repositories.owner_id = users.id").
			Where("users.name LIKE ? OR users.employee_id LIKE ? OR users.email LIKE ?", "%"+owner+"%", "%"+owner+"%", "%"+owner+"%")
	}
	if status != "" {
		query = query.Where("task_reports.status = ?", status)
	}
	if search != "" {
		query = query.Where("repositories.name LIKE ? OR task_reports.ai_summary LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Session(&gorm.Session{}).Count(&total)

	var reports []models.TaskReport
	offset := (page - 1) * pageSize
	query.Preload("Repo").Preload("Repo.Department").Preload("Repo.Owner").Preload("TaskType").
		Order("task_reports.created_at desc").Offset(offset).Limit(pageSize).Find(&reports)

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
	content, err := os.ReadFile(report.GetAbsReportPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read report file"})
		return
	}
	c.String(http.StatusOK, string(content))
}

// GetTaskReportSynthesisJSON returns the synthesis JSON file contents
func GetTaskReportSynthesisJSON(c *gin.Context) {
	id := c.Param("id")
	var report models.TaskReport
	if err := models.DB.Preload("Repo").First(&report, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}
	if report.ReportPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report path is missing"})
		return
	}

	safeRepoName := strings.ReplaceAll(report.Repo.Name, "/", "-")
	synthesisInputPath := filepath.Join(filepath.Dir(report.GetAbsReportPath()), fmt.Sprintf("report-%d-synthesis-%s.json", report.ID, safeRepoName))

	content, err := os.ReadFile(synthesisInputPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Synthesis JSON file not found"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=report-%d-synthesis-%s.json", report.ID, safeRepoName))
	c.Data(http.StatusOK, "application/json; charset=utf-8", content)
}

// GetTaskReportSummaryJSON returns the summary JSON file contents (execution trace metrics)
func GetTaskReportSummaryJSON(c *gin.Context) {
	id := c.Param("id")
	var report models.TaskReport
	if err := models.DB.Preload("Repo").First(&report, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}
	if report.ReportPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report path is missing"})
		return
	}

	safeRepoName := strings.ReplaceAll(report.Repo.Name, "/", "-")
	summaryPath := filepath.Join(filepath.Dir(report.GetAbsReportPath()), fmt.Sprintf("report-%d-summary-%s.json", report.ID, safeRepoName))

	content, err := os.ReadFile(summaryPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Summary JSON file not found"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", content)
}

func getFindingsForReport(reportID string) ([]models.AnalysisFinding, error) {
	var report models.TaskReport
	if err := models.DB.Preload("Repo").First(&report, reportID).Error; err != nil {
		return nil, err
	}

	var findings []models.AnalysisFinding

	// First: Try reading from synthesis JSON file on disk
	if report.ReportPath != "" {
		safeRepoName := strings.ReplaceAll(report.Repo.Name, "/", "-")
		synthesisPath := filepath.Join(filepath.Dir(report.GetAbsReportPath()), fmt.Sprintf("report-%d-synthesis-%s.json", report.ID, safeRepoName))

		if _, err := os.Stat(synthesisPath); err == nil {
			data, err := os.ReadFile(synthesisPath)
			if err == nil {
				if err := json.Unmarshal(data, &findings); err == nil {
					return findings, nil
				}
			}
		}
	}

	// Second: Fallback to database for legacy tasks or if file reading failed
	if err := models.DB.Where("task_report_id = ?", reportID).Order("id asc").Find(&findings).Error; err != nil {
		return nil, err
	}

	return findings, nil
}

// GetAnalysisFindings returns structured analysis findings for a task report
func GetAnalysisFindings(c *gin.Context) {
	id := c.Param("id")
	findings, err := getFindingsForReport(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load findings: " + err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, findings)
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

	var count int64
	models.DB.Model(&models.TaskExecutionLog{}).
		Where("repo_id = ? AND task_type_id = ? AND status IN (?, ?)",
			req.RepoID, req.TaskTypeID, "pending", "running").
		Count(&count)

	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该任务已经在排队或执行中，请勿重复触发"})
		return
	}

	services.EnqueueTask(nil, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})

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

	var specificEmail string
	if userID, exists := c.Get("userID"); exists {
		var user models.User
		if err := models.DB.First(&user, userID).Error; err == nil && !user.IsAdmin {
			if _, err := mail.ParseAddress(user.Email); err == nil {
				specificEmail = user.Email
			}
		}
	}

	result := services.TaskResult{
		Score:   report.Score,
		Summary: report.AISummary,
	}

	services.NotifyTaskResult(report.Repo, report.TaskType, result, specificEmail, report.ID, report.GetAbsReportPath())

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
		query = query.Where("repositories.department_id = ?", teamID)
	}
	if serviceGroup != "" {
		query = query.Where("repositories.service_group LIKE ?", "%"+serviceGroup+"%")
	}
	if owner != "" {
		query = query.Joins("LEFT JOIN users ON repositories.owner_id = users.id").
			Where("users.name LIKE ? OR users.employee_id LIKE ? OR users.email LIKE ?", "%"+owner+"%", "%"+owner+"%", "%"+owner+"%")
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
		Select("repositories.*, tr.id as latest_task_id, tr.status as latest_task_status, tr.created_at as latest_task_time, tr.score as latest_task_score, tr.task_type_id, COALESCE(rc.cnt, 0) as report_count, tr.total_chunks as latest_total_chunks, tr.processed_chunks as latest_processed_chunks, tr.success_chunks as latest_success_chunks").
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
		LatestTaskID          *uint
		LatestTaskStatus      *string
		LatestTaskTime        *string
		LatestTaskScore       *int
		TaskTypeID            *uint
		ReportCount           int
		LatestTotalChunks     *int
		LatestProcessedChunks *int
		LatestSuccessChunks   *int
	}

	var results []ResultItem
	offset := (page - 1) * pageSize
	query.Preload("Department").Preload("Owner").Offset(offset).Limit(pageSize).Find(&results)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	type OverviewItem struct {
		Repo             models.Repository `json:"repo"`
		LatestTaskID     uint              `json:"latest_task_id"`
		LatestTaskStatus string            `json:"latest_task_status"`
		LatestTaskTime   string            `json:"latest_task_time"`
		LatestTaskScore  int               `json:"latest_task_score"`
		TaskTypeID       uint              `json:"task_type_id"`
		ReportCount      int               `json:"report_count"`
		TotalChunks      int               `json:"total_chunks"`
		ProcessedChunks  int               `json:"processed_chunks"`
		SuccessChunks    int               `json:"success_chunks"`
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
			if res.LatestTotalChunks != nil {
				item.TotalChunks = *res.LatestTotalChunks
			}
			if res.LatestProcessedChunks != nil {
				item.ProcessedChunks = *res.LatestProcessedChunks
			}
			if res.LatestSuccessChunks != nil {
				item.SuccessChunks = *res.LatestSuccessChunks
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

// ClearInvalidReports deletes all report records that are not in the "success" or "skipped" state
func ClearInvalidReports(c *gin.Context) {
	var reports []models.TaskReport
	err := models.DB.Preload("TaskType").Where("status IN (?, ?, ?)", models.StatusPending, models.StatusQueued, models.StatusFailed).Find(&reports).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询无效报告失败: " + err.Error()})
		return
	}

	if len(reports) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message": "暂无需要清除的无效报告记录",
			"deleted": 0,
		})
		return
	}

	// 收集报告 ID 列表
	var reportIDs []uint
	for _, r := range reports {
		reportIDs = append(reportIDs, r.ID)
	}

	// 在数据库事务中彻底清除关联日志、Findings 和报告本身
	err = models.DB.Transaction(func(tx *gorm.DB) error {
		// 删除执行日志
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.TaskExecutionLog{}).Error; err != nil {
			return err
		}
		// 删除通用的 AnalysisFinding
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.AnalysisFinding{}).Error; err != nil {
			return err
		}
		// 删除各专项 Findings (级联清理)
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.CoredumpFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.FloatFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.ThreadFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.CjsonFinding{}).Error; err != nil {
			return err
		}
		// 最后删除 TaskReport 自身记录
		if err := tx.Where("id IN ?", reportIDs).Delete(&models.TaskReport{}).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清除失败: " + err.Error()})
		return
	}

	// 清理物理磁盘上的所有报告和临时文件
	for _, report := range reports {
		services.CleanReportFiles(report.TaskType.Name, report.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "清除成功",
		"deleted": len(reports),
	})
}

// ResumeTask resumes a failed chunked task by retrying only the failed chunks
func ResumeTask(c *gin.Context) {
	reportID := c.Param("id")

	var report models.TaskReport
	if err := models.DB.Preload("TaskType").First(&report, reportID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}

	if report.Status != "failed" && report.Status != "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有失败或成功状态的任务才能恢复"})
		return
	}

	if report.TaskType.EngineMode != "chunked" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持分片模式(chunked)的任务恢复"})
		return
	}

	// Enqueue resume task into the worker queue
	if err := services.EnqueueResumeTask(report); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "恢复任务已入队，等待排队执行"})
}

// GetPublicTaskDetails returns a single task report without auth
func GetPublicTaskDetails(c *gin.Context) {
	id := c.Param("id")
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetPublicAnalysisFindings returns structured analysis findings for a task report without auth
func GetPublicAnalysisFindings(c *gin.Context) {
	id := c.Param("id")
	findings, err := getFindingsForReport(id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load findings: " + err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, findings)
}

// TriggerMissingTasks triggers tasks for active repositories that have not undergone the task in the past N days
func TriggerMissingTasks(c *gin.Context) {
	var req struct {
		TaskTypeID uint `json:"task_type_id" binding:"required"`
		Days       int  `json:"days" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var taskType models.TaskType
	if err := models.DB.First(&taskType, req.TaskTypeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务类型不存在"})
		return
	}

	// 1. Find all active repositories
	var repos []models.Repository
	if err := models.DB.Where("is_active = ?", true).Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取代码仓失败: " + err.Error()})
		return
	}

	// 2. Query repo IDs that have scan reports of this task type in the last N days
	var scannedRepoIDs []uint
	timeLimit := time.Now().AddDate(0, 0, -req.Days)
	if err := models.DB.Model(&models.TaskReport{}).
		Where("task_type_id = ? AND created_at >= ?", req.TaskTypeID, timeLimit).
		Pluck("repo_id", &scannedRepoIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务报告失败: " + err.Error()})
		return
	}

	// 3. Filter repos to only those that have not been checked in the last N days
	scannedMap := make(map[uint]bool)
	for _, rid := range scannedRepoIDs {
		scannedMap[rid] = true
	}

	var missingRepos []models.Repository
	for _, r := range repos {
		if !scannedMap[r.ID] {
			missingRepos = append(missingRepos, r)
		}
	}

	if len(missingRepos) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("所有代码仓在过去 %d 天内均已完成 [%s] 扫描任务，无需补扫", req.Days, taskType.DisplayName)})
		return
	}

	// 4. Enqueue tasks for each missing repo
	for _, repo := range missingRepos {
		services.EnqueueTask(nil, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("成功为 %d 个代码仓触发 [%s] 补扫任务", len(missingRepos), taskType.DisplayName),
	})
}

// DeleteTaskReport deletes a single task report and all of its database & disk artifacts
func DeleteTaskReport(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的报告 ID"})
		return
	}

	var report models.TaskReport
	if err := models.DB.Preload("TaskType").First(&report, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务报告不存在"})
		return
	}

	// 1. 清理物理磁盘上的所有报告和临时文件
	services.CleanReportFiles(report.TaskType.Name, report.ID)

	// 2. 在数据库事务中彻底清除此报告相关的一切 Findings、执行日志和报告记录本身
	err = models.DB.Transaction(func(tx *gorm.DB) error {
		// 删除执行日志
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.TaskExecutionLog{}).Error; err != nil {
			return err
		}
		// 删除通用的 AnalysisFinding
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.AnalysisFinding{}).Error; err != nil {
			return err
		}
		// 删除各专项 Findings (级联清理)
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.CoredumpFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.FloatFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.ThreadFinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.CjsonFinding{}).Error; err != nil {
			return err
		}
		// 最后删除 TaskReport 自身记录
		if err := tx.Delete(&models.TaskReport{}, report.ID).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除报告失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "报告及关联文件已成功删除"})
}
