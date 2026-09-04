package services

import (
	"code-shield/models"
	"code-shield/services/governance"
)

// CalibrateSeverityDeterministically 依据确定性规则决策树计算严重级别，委托给 governance 子包
func CalibrateSeverityDeterministically(category string, verdict string, codeSnippet string) (string, string) {
	return governance.CalibrateSeverityDeterministically(category, verdict, codeSnippet)
}

// CalibrateFindings 批量校准 findings 列表的严重程度，委托给 governance 子包
func CalibrateFindings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	return governance.CalibrateFindings(findings)
}
