package handlers

import (
	"code-shield/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetIssues(c *gin.Context) {
	var issues []models.KeyIssue
	repoID := c.Query("repo_id")
	issueType := c.Query("issue_type")

	query := models.DB.Preload("Repo").Preload("TaskReport").Preload("Assignee")
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}
	if issueType != "" {
		query = query.Where("issue_type = ?", issueType)
	}
	query.Order("created_at desc").Find(&issues)

	c.JSON(http.StatusOK, issues)
}

func CreateIssue(c *gin.Context) {
	var issue models.KeyIssue
	if err := c.ShouldBindJSON(&issue); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.DB.Create(&issue).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, issue)
}

func UpdateIssue(c *gin.Context) {
	id := c.Param("id")
	var issue models.KeyIssue
	if err := models.DB.First(&issue, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Issue not found"})
		return
	}

	var req struct {
		Status     *string `json:"status"`
		AssigneeID *string `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Status != nil {
		issue.Status = *req.Status
	}
	if req.AssigneeID != nil {
		issue.AssigneeID = *req.AssigneeID
	}

	models.DB.Save(&issue)
	c.JSON(http.StatusOK, issue)
}
