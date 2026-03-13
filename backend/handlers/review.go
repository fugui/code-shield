package handlers

import (
	"code-shield/models"
	"code-shield/services"
	"net/http"
	"os"

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
