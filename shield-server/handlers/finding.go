package handlers

import (
	"code-shield/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// GetFindings returns a paginated list of analysis findings with filters
func GetFindings(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	repoID := c.Query("repo_id")
	taskTypeID := c.Query("task_type_id")
	severity := c.Query("severity")
	category := c.Query("category")
	status := c.Query("status")
	keyword := c.Query("keyword")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := models.DB.Model(&models.AnalysisFinding{})
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

	var total int64
	query.Count(&total)

	var findings []models.AnalysisFinding
	offset := (page - 1) * pageSize
	query.Order("id desc").Offset(offset).Limit(pageSize).Find(&findings)

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	c.JSON(http.StatusOK, gin.H{
		"items":      findings,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
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
