package handlers

import (
	"code-shield/models"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

func buildFindingsQuery(c *gin.Context) *gorm.DB {
	repoID := c.Query("repo_id")
	taskTypeID := c.Query("task_type_id")
	severity := c.Query("severity")
	category := c.Query("category")
	status := c.Query("status")
	keyword := c.Query("keyword")
	teamID := c.Query("team_id")

	query := models.DB.Model(&models.AnalysisFinding{})
	if teamID != "" {
		query = query.Where("repo_id IN (?)", models.DB.Model(&models.Repository{}).Select("id").Where("team_id = ?", teamID))
	}
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}
	if taskTypeID != "" {
		query = query.Where("task_type_id = ?", taskTypeID)
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title LIKE ? OR detail LIKE ? OR file_path LIKE ?", like, like, like)
	}
	return query
}

func getFindingsSortOrder(c *gin.Context) string {
	sortBy := c.DefaultQuery("sort_by", "id")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	allowedSortFields := map[string]bool{
		"id":             true,
		"severity":       true,
		"status":         true,
		"created_at":     true,
		"repo_id":        true,
		"task_report_id": true,
	}
	if !allowedSortFields[sortBy] {
		sortBy = "id"
	}

	orderClause := sortBy + " " + sortOrder
	if sortBy != "id" {
		orderClause += ", id desc"
	}
	return orderClause
}

// GetFindings returns a paginated list of analysis findings with filters
func GetFindings(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := buildFindingsQuery(c)

	var total int64
	query.Count(&total)

	orderClause := getFindingsSortOrder(c)

	var findings []models.AnalysisFinding
	offset := (page - 1) * pageSize
	query.Order(orderClause).Offset(offset).Limit(pageSize).Find(&findings)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      findings,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// ExportFindings exports all matching findings to an Excel file
func ExportFindings(c *gin.Context) {
	query := buildFindingsQuery(c)
	orderClause := getFindingsSortOrder(c)

	var findings []models.AnalysisFinding
	if err := query.Order(orderClause).Find(&findings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings"})
		return
	}

	// Fetch repos, task types, and members for name resolution
	var repos []models.Repository
	models.DB.Find(&repos)
	repoMap := make(map[uint]string)
	for _, r := range repos {
		repoMap[r.ID] = r.Name
	}

	var taskTypes []models.TaskType
	models.DB.Find(&taskTypes)
	ttMap := make(map[uint]string)
	for _, tt := range taskTypes {
		ttMap[tt.ID] = tt.DisplayName
	}

	var members []models.Member
	models.DB.Find(&members)
	memberMap := make(map[string]string)
	for _, m := range members {
		memberMap[fmt.Sprintf("%d", m.ID)] = m.Name
	}

	f := excelize.NewFile()
	sheet := "Findings"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"ID", "报告 ID", "任务类型", "代码仓", "级别", "分类", "标题", "文件路径", "行号", "详细描述", "修复建议", "状态", "处理人", "反馈", "发现时间"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	statusLabels := map[string]string{
		"open":       "待处理",
		"processing": "处理中",
		"closed":     "已关闭",
	}

	for i, finding := range findings {
		row := i + 2
		vals := []interface{}{
			finding.ID,
			finding.TaskReportID,
			ttMap[finding.TaskTypeID],
			repoMap[finding.RepoID],
			finding.Severity,
			finding.Category,
			finding.Title,
			finding.FilePath,
			finding.LineNumber,
			finding.Detail,
			finding.Suggestion,
			statusLabels[finding.Status],
			memberMap[finding.AssigneeID],
			finding.Feedback,
			finding.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		for j, val := range vals {
			cell, _ := excelize.CoordinatesToCellName(j+1, row)
			f.SetCellValue(sheet, cell, val)
		}
	}

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=findings_export.xlsx")
	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write excel file"})
	}
}

// UpdateFinding updates status, assignee, or feedback for a finding
func UpdateFinding(c *gin.Context) {
	id := c.Param("id")
	var finding models.AnalysisFinding
	if err := models.DB.First(&finding, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	var req struct {
		Status     *string `json:"status"`
		AssigneeID *string `json:"assignee_id"`
		Feedback   *string `json:"feedback"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.AssigneeID != nil {
		updates["assignee_id"] = *req.AssigneeID
	}
	if req.Feedback != nil {
		updates["feedback"] = *req.Feedback
		now := time.Now()
		updates["feedback_at"] = &now
	}

	if len(updates) > 0 {
		models.DB.Model(&finding).Updates(updates)
	}

	// Reload with updated values
	models.DB.First(&finding, id)
	c.JSON(http.StatusOK, finding)
}

// GetFindingStats returns aggregate stats for dashboard/filters
func GetFindingStats(c *gin.Context) {
	type SeverityCount struct {
		Severity string `json:"severity"`
		Count    int64  `json:"count"`
	}
	type StatusCount struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}

	var severities []SeverityCount
	models.DB.Model(&models.AnalysisFinding{}).Select("severity, count(*) as count").Group("severity").Find(&severities)

	var statuses []StatusCount
	models.DB.Model(&models.AnalysisFinding{}).Select("status, count(*) as count").Group("status").Find(&statuses)

	c.JSON(http.StatusOK, gin.H{
		"by_severity": severities,
		"by_status":   statuses,
	})
}
