package handlers

import (
	"code-shield/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type WorkbenchFinding struct {
	ID          uint        `json:"id"`
	Type        string      `json:"type"`      // "ut", "coredump", "float", "thread", "cjson"
	TypeName    string      `json:"type_name"` // "测试用例有效性", "Coredump 风险", etc.
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
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

func GetMyFindings(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	uid := userID.(uint)
	list := []WorkbenchFinding{}

	// 1. TestCaseFinding (ut)
	var utFindings []models.TestCaseFinding
	models.DB.Preload("Repo").Where("assignee_id = ?", uid).Find(&utFindings)
	for _, f := range utFindings {
		list = append(list, WorkbenchFinding{
			ID:          f.ID,
			Type:        "ut",
			TypeName:    "测试用例有效性",
			RepoID:      f.RepoID,
			RepoName:    f.Repo.Name,
			RepoURL:     f.Repo.URL,
			FilePath:    f.FilePath,
			LineNumber:  f.LineNumber,
			Title:       f.TestCaseName,
			Detail:      f.Detail,
			Severity:    f.Severity,
			Category:    f.Category,
			CodeSnippet: f.CodeSnippet,
			Suggestion:  f.Suggestion,
			Status:      f.Status,
			StatusLog:   f.StatusLog,
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	// 2. CoredumpFinding (coredump)
	var coredumpFindings []models.CoredumpFinding
	models.DB.Preload("Repo").Where("assignee_id = ?", uid).Find(&coredumpFindings)
	for _, f := range coredumpFindings {
		list = append(list, WorkbenchFinding{
			ID:          f.ID,
			Type:        "coredump",
			TypeName:    "Coredump 风险",
			RepoID:      f.RepoID,
			RepoName:    f.Repo.Name,
			RepoURL:     f.Repo.URL,
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
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	// 3. FloatFinding (float)
	var floatFindings []models.FloatFinding
	models.DB.Preload("Repo").Where("assignee_id = ?", uid).Find(&floatFindings)
	for _, f := range floatFindings {
		list = append(list, WorkbenchFinding{
			ID:          f.ID,
			Type:        "float",
			TypeName:    "Python 浮点数比较",
			RepoID:      f.RepoID,
			RepoName:    f.Repo.Name,
			RepoURL:     f.Repo.URL,
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
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	// 4. ThreadFinding (thread)
	var threadFindings []models.ThreadFinding
	models.DB.Preload("Repo").Where("assignee_id = ?", uid).Find(&threadFindings)
	for _, f := range threadFindings {
		list = append(list, WorkbenchFinding{
			ID:          f.ID,
			Type:        "thread",
			TypeName:    "显式创建线程",
			RepoID:      f.RepoID,
			RepoName:    f.Repo.Name,
			RepoURL:     f.Repo.URL,
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
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	// 5. CjsonFinding (cjson)
	var cjsonFindings []models.CjsonFinding
	models.DB.Preload("Repo").Where("assignee_id = ?", uid).Find(&cjsonFindings)
	for _, f := range cjsonFindings {
		list = append(list, WorkbenchFinding{
			ID:          f.ID,
			Type:        "cjson",
			TypeName:    "cJSON 内存泄漏",
			RepoID:      f.RepoID,
			RepoName:    f.Repo.Name,
			RepoURL:     f.Repo.URL,
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
			CreatedAt:   f.CreatedAt,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, list)
}
