package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetReviews(c *gin.Context) {
	var reviews []models.ReviewReport
	repoID := c.Query("repo_id")
	query := models.DB.Preload("Repo")
	if repoID != "" {
		query = query.Where("repo_id = ?", repoID)
	}
	query.Order("created_at desc").Find(&reviews)
	c.JSON(http.StatusOK, reviews)
}

func GetReviewDetails(c *gin.Context) {
	id := c.Param("id")
	var review models.ReviewReport
	if err := models.DB.Preload("Repo").First(&review, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Review not found"})
		return
	}
	c.JSON(http.StatusOK, review)
}

// GetReviewReportMarkdown returns the raw markdown string of the AI review report
func GetReviewReportMarkdown(c *gin.Context) {
	id := c.Param("id")
	var review models.ReviewReport
	if err := models.DB.First(&review, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Review not found"})
		return
	}

	if review.ReportPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report tracking path is missing"})
		return
	}

	content, err := os.ReadFile(review.ReportPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read report file"})
		return
	}

	c.String(http.StatusOK, string(content))
}

func TriggerReview(c *gin.Context) {
	// Manual trigger logic
	var req struct {
		RepoID uint `json:"repo_id" binding:"required"`
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

	// Trigger the async AI review process (manual trigger does not auto-notify)
	services.EnqueueReviewTask(nil, repo.ID, repo.URL, false, "manual")

	c.JSON(http.StatusAccepted, gin.H{"message": "AI Review started in the background"})
}

// TriggerManualNotification sends the notification webhook explicitly for a specific review report
func TriggerManualNotification(c *gin.Context) {
	reportID := c.Param("id")

	var report models.ReviewReport
	if err := models.DB.Preload("Repo").First(&report, reportID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Review report not found"})
		return
	}

	msg := "Manually triggered notification."
	if report.Status == "success" {
		msg = "Review completed."
	} else if report.Status == "failed" {
		msg = "Review failed."
	}

	mdContent := ""
	if report.Status == "success" && report.ReportPath != "" {
		contentBytes, err := os.ReadFile(report.ReportPath)
		if err == nil {
			mdContent = string(contentBytes)
		}
	}

	// Send the payload to the Windows Node.js service
	services.NotifyNotifier(report.Repo.ID, report.Status, msg, mdContent)

	c.JSON(http.StatusOK, gin.H{"message": "Notification dispatched."})
}

// GetReviewOverview returns a paginated list of repositories appended with their most recent review statistics.
func GetReviewOverview(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	teamID := c.Query("team_id")
	serviceGroup := c.Query("service_group")
	owner := c.Query("owner")
	sort := c.DefaultQuery("sort", "latest_review_time_desc")

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

	// Subquery to get the latest review report ID for each repository
	subQuery := models.DB.Model(&models.ReviewReport{}).
		Select("MAX(id)").
		Group("repo_id")

	// Main query joining repositories with latest review report
	query = query.
		Select("repositories.*, rr.id as latest_review_id, rr.status as latest_review_status, rr.created_at as latest_review_time, rr.critical_issues, rr.major_issues, rr.minor_issues").
		Joins("LEFT JOIN review_reports rr ON rr.id IN (?) AND rr.repo_id = repositories.id", subQuery)

	if sort == "latest_review_time_desc" {
		query = query.Order("latest_review_time DESC NULLS LAST, repositories.id DESC")
	} else if sort == "latest_review_time_asc" {
		query = query.Order("latest_review_time ASC NULLS LAST, repositories.id ASC")
	} else {
		query = query.Order("repositories.id DESC")
	}

	type ResultItem struct {
		models.Repository
		LatestReviewID     *uint
		LatestReviewStatus *string
		LatestReviewTime   *string
		CriticalIssues     *int
		MajorIssues        *int
		MinorIssues        *int
	}

	var results []ResultItem
	offset := (page - 1) * pageSize
	query.Preload("Team").Preload("Owner").Offset(offset).Limit(pageSize).Find(&results)
	
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	type OverviewItem struct {
		Repo               models.Repository `json:"repo"`
		LatestReviewID     uint              `json:"latest_review_id"`
		LatestReviewStatus string            `json:"latest_review_status"`
		LatestReviewTime   string            `json:"latest_review_time"`
		CriticalIssues     int               `json:"critical_issues"`
		MajorIssues        int               `json:"major_issues"`
		MinorIssues        int               `json:"minor_issues"`
	}

	var items []OverviewItem
	for _, res := range results {
		item := OverviewItem{
			Repo: res.Repository,
		}
		
		if res.LatestReviewStatus != nil {
			if res.LatestReviewID != nil {
				item.LatestReviewID = *res.LatestReviewID
			}
			item.LatestReviewStatus = *res.LatestReviewStatus
			if res.LatestReviewTime != nil {
				// We need to parse and format the string. In SQLite, datetime is string.
				// The gorm Find might parse it to string directly if the struct type is string.
				// Format it if needed, but since it's already a string, we can just use it, or substring it.
				t := *res.LatestReviewTime
				if len(t) > 19 {
					t = t[:19] // Keep only "YYYY-MM-DD HH:MM:SS"
				}
				item.LatestReviewTime = t
			}
			if res.CriticalIssues != nil { item.CriticalIssues = *res.CriticalIssues }
			if res.MajorIssues != nil { item.MajorIssues = *res.MajorIssues }
			if res.MinorIssues != nil { item.MinorIssues = *res.MinorIssues }
		} else {
			item.LatestReviewStatus = "none"
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

