package services

import (
	"code-shield/models"
	"fmt"
	"time"
)

// MarkDefectFeedback 处理研发人员对缺陷的反馈（误报/不予修复/已确认）
func MarkDefectFeedback(repoID uint, taskTypeID uint, fingerprint string, feedbackStatus string, reason string, userID *uint) error {
	if models.DB == nil {
		return nil
	}

	now := time.Now()
	var record models.DefectFingerprintRecord
	err := models.DB.Where("repo_id = ? AND task_type_id = ? AND fingerprint = ?", repoID, taskTypeID, fingerprint).First(&record).Error
	if err != nil {
		return fmt.Errorf("defect fingerprint %s not found: %w", fingerprint, err)
	}

	// 1. 更新指纹记忆库中的反馈状态
	updates := map[string]interface{}{
		"feedback_status":  feedbackStatus,
		"feedback_reason":  reason,
		"feedback_user_id": userID,
		"feedback_at":      &now,
	}
	if err := models.DB.Model(&record).Updates(updates).Error; err != nil {
		return err
	}

	// 2. 如果标记为误报 (FALSE_POSITIVE) 或不予修复 (WONT_FIX)，自动沉淀为代码仓负样本例外规则
	if feedbackStatus == "FALSE_POSITIVE" || feedbackStatus == "WONT_FIX" {
		rule := models.RepoFeedbackRule{
			RepoID:     repoID,
			TaskTypeID: taskTypeID,
			ScopeType:  "FILE",
			Pattern:    record.FilePath,
			RuleAction: "IGNORE",
			Reason:     fmt.Sprintf("[%s] %s (由指纹 %s 沉淀)", feedbackStatus, reason, fingerprint[:8]),
			CreatedBy:  "System-Feedback",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		_ = models.DB.Create(&rule).Error
	}

	// 3. 同步更新最新关联的 AnalysisFinding / CampaignFinding
	models.DB.Model(&models.AnalysisFinding{}).Where("fingerprint = ?", fingerprint).Updates(map[string]interface{}{
		"feedback":    fmt.Sprintf("[%s] %s", feedbackStatus, reason),
		"feedback_at": &now,
	})

	return nil
}

// GetNegativeRulesForScan 获取指定仓库和任务类型在扫描时应注入的负样本规则列表
func GetNegativeRulesForScan(repoID uint, taskTypeID uint) []string {
	if models.DB == nil {
		return nil
	}

	var rules []models.RepoFeedbackRule
	models.DB.Where("repo_id = ? AND (task_type_id = ? OR scope_type = 'GLOBAL')", repoID, taskTypeID).Find(&rules)

	var formatted []string
	for _, r := range rules {
		formatted = append(formatted, fmt.Sprintf("[%s] 匹配: %s | 原因: %s", r.ScopeType, r.Pattern, r.Reason))
	}
	return formatted
}
