package handlers

import (
	commonAudit "code-common/backend/audit"
	"code-shield/models"
	"code-shield/services"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
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

func getOperatorInfo(c *gin.Context) (operatorID *uint, operatorName string, clientIP string) {
	clientIP = c.ClientIP()
	if userIDVal, exists := c.Get("userID"); exists {
		if uid, ok := userIDVal.(uint); ok && uid > 0 {
			operatorID = &uid
			var user models.User
			if err := models.DB.First(&user, uid).Error; err == nil {
				operatorName = user.Name
				if operatorName == "" {
					operatorName = user.Email
				}
			}
		}
	}
	if operatorName == "" {
		if name, exists := c.Get("username"); exists {
			operatorName = fmt.Sprintf("%v", name)
		} else {
			operatorName = "系统用户"
		}
	}
	return
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
		Where("repo_id = ? AND task_type_id = ? AND status NOT IN (?, ?, ?)",
			req.RepoID, req.TaskTypeID, models.StatusSuccess, models.StatusFailed, models.StatusSkipped).
		Count(&count)

	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该任务已经在排队或执行中，请勿重复触发"})
		return
	}

	opID, opName, clientIP := getOperatorInfo(c)
	batchNo := fmt.Sprintf("TRG-%s-%d", time.Now().Format("20060102150405"), repo.ID)

	triggerLog := models.TaskTriggerLog{
		TriggerBatch:  batchNo,
		TriggerType:   "manual_single",
		OperatorID:    opID,
		OperatorName:  opName,
		TaskTypeID:    taskType.ID,
		TargetMode:    "single",
		TargetSummary: fmt.Sprintf("代码仓: %s", repo.Name),
		TotalRepos:    1,
		SuccessCount:  1,
		ClientIP:      clientIP,
		CreatedAt:     time.Now(),
	}
	models.DB.Create(&triggerLog)

	var tLogID *uint
	if triggerLog.ID > 0 {
		tLogID = &triggerLog.ID
	}

	services.EnqueueTaskWithTriggerLog(nil, tLogID, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})

	commonAudit.SetAuditContext(c, "scan", "trigger", models.AuditLevelP1,
		fmt.Sprintf("触发单仓扫描 [%s] 代码仓: %s", taskType.DisplayName, repo.Name),
		"task_trigger_log", fmt.Sprintf("%d", triggerLog.ID), triggerLog.TriggerBatch,
		nil, triggerLog)

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
		if err := models.DB.First(&user, userID).Error; err == nil && !user.HasRole("shield_admin") {
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
	query.Session(&gorm.Session{}).Count(&total)

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

	// 收集报告 ID 列表，并取消所有可能正在运行的关联任务
	var reportIDs []uint
	for _, r := range reports {
		reportIDs = append(reportIDs, r.ID)
		// 无条件尝试取消：防止正在运行的任务在 Report 被删后继续产生孤儿进程或数据不一致
		services.CancelRunningTask(r.ID)
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
		// 删除统一专项 CampaignFinding (级联清理)
		if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.CampaignFinding{}).Error; err != nil {
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

// TriggerMissingTasks triggers tasks for active repositories that have not undergone the task in the past N days (or all repositories when days <= 0)
func TriggerMissingTasks(c *gin.Context) {
	var req struct {
		TaskTypeID   uint   `json:"task_type_id" binding:"required"`
		Days         int    `json:"days"`
		ServiceGroup string `json:"service_group"`
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

	// 1. Find all active repositories (optionally filtered by service_group)
	var repos []models.Repository
	dbQuery := models.DB.Where("is_active = ?", true)
	if req.ServiceGroup != "" {
		dbQuery = dbQuery.Where("service_group = ?", req.ServiceGroup)
	}
	if err := dbQuery.Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取代码仓失败: " + err.Error()})
		return
	}

	var targetRepos []models.Repository
	if req.Days <= 0 {
		// 全量扫描：不限时间范围，覆盖所有匹配的活跃仓库
		targetRepos = repos
	} else {
		// 2. Query repo IDs whose latest scan report in the last N days is not failed (i.e. success, skipped, or in progress)
		// 过去 N 天内，按 repo_id 分组找出每个代码仓最新的一条任务报告 ID
		timeLimit := time.Now().AddDate(0, 0, -req.Days)
		latestSubQuery := models.DB.Model(&models.TaskReport{}).
			Select("MAX(id)").
			Where("task_type_id = ? AND created_at >= ?", req.TaskTypeID, timeLimit).
			Group("repo_id")

		// 排除掉那些“最新报告不是 failed”的代码仓（即已成功 success、已跳过 skipped 或正在运行中的代码仓）；
		// 若过去 N 天内未曾扫描，或最新一次扫描结果为 failed（执行失败未恢复），则必须纳入补扫
		var excludedRepoIDs []uint
		if err := models.DB.Model(&models.TaskReport{}).
			Where("id IN (?) AND status != ?", latestSubQuery, models.StatusFailed).
			Pluck("repo_id", &excludedRepoIDs).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询任务报告失败: " + err.Error()})
			return
		}

		// 3. Filter repos: 仅保留未扫描或最新扫描失败的代码仓进行补扫
		excludedMap := make(map[uint]bool, len(excludedRepoIDs))
		for _, rid := range excludedRepoIDs {
			excludedMap[rid] = true
		}

		for _, r := range repos {
			if !excludedMap[r.ID] {
				targetRepos = append(targetRepos, r)
			}
		}
	}

	if len(targetRepos) == 0 {
		if req.Days <= 0 {
			c.JSON(http.StatusOK, gin.H{"message": "未找到匹配的活跃代码仓，无需扫描"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("所有代码仓在过去 %d 天内均已完成 [%s] 扫描任务，无需补扫", req.Days, taskType.DisplayName)})
		}
		return
	}

	opID, opName, clientIP := getOperatorInfo(c)
	batchSuffix := "MISSING"
	if req.Days <= 0 {
		batchSuffix = "ALL"
	}
	batchNo := fmt.Sprintf("TRG-%s-%s", time.Now().Format("20060102150405"), batchSuffix)

	var targetSummary string
	var targetMode string
	if req.Days <= 0 {
		targetMode = "all"
		targetSummary = fmt.Sprintf("全量扫描: 全部活跃代码仓 (共 %d 个)", len(targetRepos))
	} else {
		targetMode = "missing_days"
		targetSummary = fmt.Sprintf("快速补扫: 过去 %d 天未扫代码仓 (共 %d 个)", req.Days, len(targetRepos))
	}
	if req.ServiceGroup != "" {
		targetSummary += fmt.Sprintf(" [服务组件: %s]", req.ServiceGroup)
	}

	filterParamsBytes, _ := json.Marshal(map[string]interface{}{
		"days":          req.Days,
		"service_group": req.ServiceGroup,
	})

	triggerLog := models.TaskTriggerLog{
		TriggerBatch:  batchNo,
		TriggerType:   "manual_batch",
		OperatorID:    opID,
		OperatorName:  opName,
		TaskTypeID:    taskType.ID,
		TargetMode:    targetMode,
		TargetSummary: targetSummary,
		FilterParams:  filterParamsBytes,
		TotalRepos:    len(targetRepos),
		ClientIP:      clientIP,
		CreatedAt:     time.Now(),
	}
	models.DB.Create(&triggerLog)

	var tLogID *uint
	if triggerLog.ID > 0 {
		tLogID = &triggerLog.ID
	}

	successCount := 0
	skipCount := 0

	// 4. Enqueue tasks for each target repo
	for _, repo := range targetRepos {
		ok := services.EnqueueTaskWithTriggerLog(nil, tLogID, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})
		if ok {
			successCount++
		} else {
			skipCount++
		}
	}

	if triggerLog.ID > 0 {
		models.DB.Model(&triggerLog).Updates(map[string]interface{}{
			"success_count": successCount,
			"skip_count":    skipCount,
		})
	}

	actionDesc := "补扫"
	if req.Days <= 0 {
		actionDesc = "全量扫描"
	}
	commonAudit.SetAuditContext(c, "scan", "trigger", models.AuditLevelP1,
		fmt.Sprintf("触发%s [%s] 覆盖 %d 仓 (成功 %d, 跳过 %d)", actionDesc, taskType.DisplayName, len(targetRepos), successCount, skipCount),
		"task_trigger_log", fmt.Sprintf("%d", triggerLog.ID), triggerLog.TriggerBatch,
		nil, triggerLog)

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("成功为 %d 个代码仓触发 [%s] %s任务 (成功排队 %d, 跳过 %d)", len(targetRepos), taskType.DisplayName, actionDesc, successCount, skipCount),
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
		// 删除统一专项 CampaignFinding (级联清理)
		if err := tx.Where("task_report_id = ?", report.ID).Delete(&models.CampaignFinding{}).Error; err != nil {
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

	commonAudit.SetAuditContext(c, "scan", "delete_report", models.AuditLevelP1,
		fmt.Sprintf("删除了扫描任务报告 (报告 ID: %d, 任务类型: %s, 得分: %d)", report.ID, report.TaskType.DisplayName, report.Score),
		"task_report", fmt.Sprintf("%d", report.ID), fmt.Sprintf("报告-%d", report.ID),
		report, nil)

	c.JSON(http.StatusOK, gin.H{"message": "报告及关联文件已成功删除"})
}
