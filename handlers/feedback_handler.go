package handlers

import (
	"code-shield/models"
	"code-shield/services"
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

	// 从 Context 获取当前操作用户
	var currentUserID *uint
	if userVal, exists := c.Get("currentUser"); exists {
		if u, ok := userVal.(*models.User); ok && u != nil {
			currentUserID = &u.ID
		}
	}

	// 确保指纹存在（若历史数据未生成指纹，现场计算补全）
	if finding.Fingerprint == "" {
		finding.Fingerprint = services.CalculateDefectFingerprint(finding.RepoID, finding.TaskTypeID, finding.FilePath, finding.TriggerLine, finding.ScopeSymbol)
		models.DB.Model(&finding).Update("fingerprint", finding.Fingerprint)
	}

	err = services.MarkDefectFeedback(finding.RepoID, finding.TaskTypeID, finding.Fingerprint, req.FeedbackStatus, req.Reason, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Feedback submitted and memory rule updated successfully",
		"fingerprint": finding.Fingerprint,
	})
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
