package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

// ExtractedFeedbackRule 结构化提取的负样本规则
type ExtractedFeedbackRule struct {
	ScopeType  string `json:"scope_type"`  // "FILE" 或 "SYMBOL"
	Pattern    string `json:"pattern"`     // 文件路径或函数符号
	RuleAction string `json:"rule_action"` // "IGNORE"
	Reason     string `json:"reason"`      // 提炼的免扫理由
}

// ExtractFeedbackRule 使用指定的 AI 驱动提炼误报特征规则
func ExtractFeedbackRule(inv invoker.AIInvoker, filePath, codeSnippet, defectTitle, userReason string, workDirOpt ...string) (*ExtractedFeedbackRule, error) {
	if inv == nil {
		return nil, fmt.Errorf("no invoker provided for feedback rule extraction")
	}

	workDir := ""
	if len(workDirOpt) > 0 && workDirOpt[0] != "" {
		workDir = workDirOpt[0]
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

	req := invoker.AIRequest{
		WorkDir:        workDir,
		PromptMsg:      prompt,
		OutputPath:     tmpPath,
		TimeoutMin:     1,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:    "知识沉淀: 负样本特征提炼",
			SubTask:  fmt.Sprintf("提炼规则 (%s)", filePath),
			TierName: "system_tool",
		},
	}

	if err := inv.Invoke(req); err != nil {
		return nil, err
	}

	outBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}

	cleaned := cleanJSONOutput(outBytes)
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

// MarkFindingFeedback 将人工反馈保存为当前 finding 注释，并可提炼扫描抑制规则。
func MarkFindingFeedback(finding *models.AnalysisFinding, feedbackStatus string, reason string, userID *uint) error {
	if models.DB == nil {
		return nil
	}
	if finding == nil || finding.ID == 0 {
		return fmt.Errorf("finding is required")
	}

	now := time.Now()
	if feedbackStatus == "FALSE_POSITIVE" || feedbackStatus == "WONT_FIX" {
		ruleScope := "FILE"
		rulePattern := finding.FilePath
		ruleReason := fmt.Sprintf("[%s] %s (由 finding %d 沉淀)", feedbackStatus, reason, finding.ID)
		snippet := finding.CodeSnippet
		title := finding.Title
		if title == "" {
			title = finding.Category
		}

		if rawInv, ok := invoker.GetRawInvoker("native"); ok && rawInv != nil {
			if extracted, extErr := ExtractFeedbackRule(rawInv, finding.FilePath, snippet, title, reason); extErr == nil && extracted != nil {
				if extracted.ScopeType != "" {
					ruleScope = extracted.ScopeType
				}
				if extracted.Pattern != "" {
					rulePattern = extracted.Pattern
				}
				if extracted.Reason != "" {
					ruleReason = fmt.Sprintf("[%s] %s (由 finding %d 提炼)", feedbackStatus, extracted.Reason, finding.ID)
				}
			}
		}

		rule := models.RepoFeedbackRule{
			RepoID:     finding.RepoID,
			TaskTypeID: finding.TaskTypeID,
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

// cleanJSONOutput 清洗 AI 输出的 JSON 文本
func cleanJSONOutput(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") {
		if start := strings.Index(s, "{"); start != -1 {
			if end := strings.LastIndex(s, "}"); end > start {
				s = s[start : end+1]
			}
		}
	}
	return []byte(s)
}
