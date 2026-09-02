package services

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"code-shield/models"
)

// ExtractedFeedbackRule 结构化提取的负样本规则
type ExtractedFeedbackRule struct {
	ScopeType  string `json:"scope_type"`  // "FILE" 或 "SYMBOL"
	Pattern    string `json:"pattern"`     // 文件路径或函数符号
	RuleAction string `json:"rule_action"` // "IGNORE"
	Reason     string `json:"reason"`      // 提炼的免扫理由
}

// ExtractFeedbackRuleViaNative 使用 Native Thin LLM 快速提炼误报特征规则
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

	invoker := GetAIInvoker(backend)
	if invoker == nil {
		return nil, fmt.Errorf("no invoker for backend: %s", backend)
	}

	prompt := fmt.Sprintf(`你是一个代码安全规则分析专家。研发人员将以下代码缺陷标记为误报。请根据上下文提炼出结构化的负样本例外规则，供后续扫描引擎避免同类误报。

# 缺陷信息
- 文件路径: %s
- 缺陷标题: %s
- 代码片段:
%s

# 研发反馈原因
%s

请以纯 JSON 格式输出，不要输出任何 Markdown 代码块包裹：
{
  "scope_type": "FILE",
  "pattern": "%s",
  "rule_action": "IGNORE",
  "reason": "提炼的简明免扫原因"
}`, filePath, defectTitle, codeSnippet, userReason, filePath)

	tmpFile, err := os.CreateTemp("", "feedback-rule-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := AIRequest{
		PromptMsg:  prompt,
		OutputPath: tmpPath,
		TimeoutMin: 1,
	}

	if err := invoker.Invoke(req); err != nil {
		return nil, err
	}

	outBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}

	cleaned := cleanJSONFromAI(outBytes)
	var rule ExtractedFeedbackRule
	if err := json.Unmarshal(cleaned, &rule); err != nil {
		return nil, err
	}
	if rule.Pattern == "" {
		rule.Pattern = filePath
	}
	if rule.ScopeType == "" {
		rule.ScopeType = "FILE"
	}
	if rule.RuleAction == "" {
		rule.RuleAction = "IGNORE"
	}
	return &rule, nil
}

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
		ruleScope := "FILE"
		rulePattern := record.FilePath
		ruleReason := fmt.Sprintf("[%s] %s (由指纹 %s 沉淀)", feedbackStatus, reason, fingerprint[:8])

		// 尝试通过 Thin LLM 智能提取更加精准的模式与原因
		var snippet, title string
		var finding models.AnalysisFinding
		if err := models.DB.Where("fingerprint = ?", fingerprint).First(&finding).Error; err == nil {
			snippet = finding.CodeSnippet
			title = finding.Title
		}
		if title == "" {
			title = record.Category
		}
		if extracted, extErr := ExtractFeedbackRuleViaNative(record.FilePath, snippet, title, reason); extErr == nil && extracted != nil {
			if extracted.ScopeType != "" {
				ruleScope = extracted.ScopeType
			}
			if extracted.Pattern != "" {
				rulePattern = extracted.Pattern
			}
			if extracted.Reason != "" {
				ruleReason = fmt.Sprintf("[%s] %s (由指纹 %s 提炼)", feedbackStatus, extracted.Reason, fingerprint[:8])
			}
		}

		rule := models.RepoFeedbackRule{
			RepoID:     repoID,
			TaskTypeID: taskTypeID,
			ScopeType:  ruleScope,
			Pattern:    rulePattern,
			RuleAction: "IGNORE",
			Reason:     ruleReason,
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
