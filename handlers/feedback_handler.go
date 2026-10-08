package handlers

import (
	"code-shield/models"
	"code-shield/services/defectlifecycle"
	"code-shield/services/governance"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type FeedbackReq struct {
	FeedbackStatus string `json:"feedback_status" binding:"required"` // FALSE_POSITIVE, WONT_FIX, CONFIRMED
	Reason         string `json:"reason" binding:"required"`          // 反馈理由
}

// SubmitFindingFeedbackHandler 处理研发人员针对指定 Finding 的反馈
func SubmitFindingFeedbackHandler(c *gin.Context) {
	idStr := c.Param("id")
	findingID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid finding ID"})
		return
	}

	var req FeedbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var finding models.AnalysisFinding
	if err := models.DB.First(&finding, findingID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
		return
	}

	var currentUserID *uint
	if userVal, exists := c.Get("currentUser"); exists {
		if user, ok := userVal.(*models.User); ok && user != nil {
			currentUserID = &user.ID
		}
	}

	if finding.ObservationGroupUID != "" {
		var observation models.DefectObservation
		if err := models.DB.Where("report_id = ? AND observation_group_uid = ?", finding.TaskReportID, finding.ObservationGroupUID).
			First(&observation).Error; err == nil && observation.DefectID != nil {
			status := "closed"
			switch req.FeedbackStatus {
			case "CONFIRMED":
				status = "open"
			case "FALSE_POSITIVE":
				status = "closed"
			case "WONT_FIX":
				status = "closed"
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid feedback status"})
				return
			}
			if _, err := defectlifecycle.UpdateDefectWorkflow(models.DB, *observation.DefectID, defectlifecycle.DefectWorkflowInput{
				Status: status, Feedback: req.Reason, ActorID: valueOrDefault(currentUserID),
			}); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

	err = governance.MarkFindingFeedback(&finding, req.FeedbackStatus, req.Reason, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Current finding feedback submitted successfully",
	})
}

func valueOrDefault(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}

// GetRepoFeedbackRulesHandler 获取代码仓专有的负样本例外规则库
func GetRepoFeedbackRulesHandler(c *gin.Context) {
	idStr := c.Param("id")
	repoID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid repo ID"})
		return
	}

	if models.DB == nil {
		c.JSON(http.StatusOK, gin.H{"items": []models.RepoFeedbackRule{}, "total": 0})
		return
	}

	var rules []models.RepoFeedbackRule
	models.DB.Where("repo_id = ? OR scope_type = 'GLOBAL'", repoID).Order("id desc").Find(&rules)

	c.JSON(http.StatusOK, gin.H{
		"items": rules,
		"total": len(rules),
	})
}

// DeleteFeedbackRuleHandler 删除一条负样本规则
func DeleteFeedbackRuleHandler(c *gin.Context) {
	ruleIDStr := c.Param("rule_id")
	ruleID, err := strconv.ParseUint(ruleIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	if err := models.DB.Delete(&models.RepoFeedbackRule{}, ruleID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Feedback rule deleted successfully"})
}
