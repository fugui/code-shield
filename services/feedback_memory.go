package services

import (
	"code-shield/models"
	"code-shield/services/governance"
)

// ExtractedFeedbackRule 结构化提取的负样本规则别名
type ExtractedFeedbackRule = governance.ExtractedFeedbackRule

// ExtractFeedbackRuleViaNative 使用 Native Thin LLM 快速提炼误报特征规则，委托给 governance
func ExtractFeedbackRuleViaNative(filePath, codeSnippet, defectTitle, userReason string) (*ExtractedFeedbackRule, error) {
	backend := models.AppConfig.AI.ToolBackends.FeedbackExtraction
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		backend = models.AppConfig.AI.Backend
	}
	if backend == "" {
		backend = "native"
	}

	inv := GetAIInvoker(backend)
	return governance.ExtractFeedbackRule(inv, filePath, codeSnippet, defectTitle, userReason)
}

// MarkDefectFeedback 处理研发人员对缺陷的反馈（误报/不予修复/已确认），委托给 governance
func MarkDefectFeedback(repoID uint, taskTypeID uint, fingerprint string, feedbackStatus string, reason string, userID *uint) error {
	return governance.MarkDefectFeedback(repoID, taskTypeID, fingerprint, feedbackStatus, reason, userID)
}

// GetNegativeRulesForScan 获取指定仓库和任务类型在扫描时应注入的负样本规则列表，委托给 governance
func GetNegativeRulesForScan(repoID uint, taskTypeID uint) []string {
	return governance.GetNegativeRulesForScan(repoID, taskTypeID)
}
