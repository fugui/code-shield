package handlers

import (
	"code-shield/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type WorkbenchFinding struct {
	ID          uint        `json:"id"`
	Type        string      `json:"type"`      // "ut", "coredump", "float", "thread", "cjson", "unordered-collection", "deep-review", etc.
	TypeName    string      `json:"type_name"` // "测试用例有效性", "Coredump 风险", etc.
	TaskTypeID  uint        `json:"task_type_id"`
	RepoID      uint        `json:"repo_id"`
	RepoName    string      `json:"repo_name"`
	RepoURL     string      `json:"repo_url"`
	FilePath    string      `json:"file_path"`
	LineNumber  string      `json:"line_number"`
	Title       string      `json:"title"`
	Detail      string      `json:"detail"`
	Severity    string      `json:"severity"`
	Category    string      `json:"category"`
	CodeSnippet string      `json:"code_snippet"`
	Suggestion  string      `json:"suggestion"`
	Status      string      `json:"status"`
	StatusLog   interface{} `json:"status_log"`
	Feedback    string      `json:"feedback"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// GetMyFindings 获取当前用户被指派的专项缺陷列表（统一查询 campaign_findings 表并支持分页）
func GetMyFindings(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	uid := userID.(uint)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "25"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 25
	}

	campaignType := c.Query("type")
	severity := c.Query("severity")
	status := c.Query("status")
	keyword := c.Query("keyword")

	query := models.DB.Model(&models.CampaignFinding{}).
		Preload("Repo").Preload("TaskType").
		Where("assignee_id = ?", uid)

	if campaignType != "" {
		var tt models.TaskType
		if err := models.DB.Where("name = ? OR campaign_path = ?", campaignType, campaignType).First(&tt).Error; err == nil {
			query = query.Where("task_type_id = ?", tt.ID)
		}
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("file_path LIKE ? OR title LIKE ? OR detail LIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count findings"})
		return
	}

	var dbFindings []models.CampaignFinding
	if err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&dbFindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch findings"})
		return
	}

	items := make([]WorkbenchFinding, 0, len(dbFindings))
	for _, f := range dbFindings {
		typeAlias := f.TaskType.CampaignPath
		if typeAlias == "" {
			typeAlias = f.TaskType.Name
		}
		repoName := ""
		repoURL := ""
		if f.Repo.Name != "" {
			repoName = f.Repo.Name
			repoURL = f.Repo.URL
		}
		items = append(items, WorkbenchFinding{
			ID:          f.ID,
			Type:        typeAlias,
			TypeName:    f.TaskType.DisplayName,
			TaskTypeID:  f.TaskTypeID,
			RepoID:      f.RepoID,
			RepoName:    repoName,
			RepoURL:     repoURL,
			FilePath:    f.FilePath,
			LineNumber:  f.LineNumber,
			Title:       f.Title,
			Detail:      f.Detail,
			Severity:    f.Severity,
			Category:    f.Category,
			CodeSnippet: f.CodeSnippet,
			Suggestion:  f.Suggestion,
			Status:      f.Status,
			StatusLog:   f.StatusLog,
			Feedback:    f.Feedback,
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"items":     items,
	})
}
