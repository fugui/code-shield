package reconciliation

import (
	"code-shield/models"
	"code-shield/services/defects"
	"regexp"
	"sort"
	"strings"
)

// SourceAnchor 物理源码锚点（统一复用 defects.SourceAnchor SSOT）
type SourceAnchor = defects.SourceAnchor

// CleanSourceToken 对代码行进行 Token 级去噪清洗 (复用 defects.CleanSourceToken 单一真实源)
func CleanSourceToken(line string) string {
	return defects.CleanSourceToken(line)
}

// NormalizeScopeSymbol 规范化作用域符号 (复用 defects.NormalizeScopeSymbol 单一真实源)
func NormalizeScopeSymbol(scope string) string {
	return defects.NormalizeScopeSymbol(scope)
}

// ParseLineNumberRange 解析行号范围 (复用 defects.ParseLineNumberRange 单一真实源)
func ParseLineNumberRange(lineStr string) (int, int) {
	return defects.ParseLineNumberRange(lineStr)
}

// CalculateDefectFingerprint 计算 L1 物理强指纹 (复用 defects.CalculateDefectFingerprint 单一真实源)
func CalculateDefectFingerprint(repoID uint, taskTypeID uint, filePath string, triggerLine string, scopeSymbol string) string {
	return defects.CalculateDefectFingerprint(repoID, taskTypeID, filePath, triggerLine, scopeSymbol)
}

// CalculateWeakScopeFingerprint 计算 L2 弱作用域指纹 (复用 defects.CalculateWeakScopeFingerprint 单一真实源)
func CalculateWeakScopeFingerprint(repoID uint, taskTypeID uint, filePath string, scopeSymbol string) string {
	return defects.CalculateWeakScopeFingerprint(repoID, taskTypeID, filePath, scopeSymbol)
}

// CalculateTokenJaccard 计算 2-gram Jaccard 相似度 [0.0, 1.0] (复用 defects.CalculateTokenJaccard 单一真实源)
func CalculateTokenJaccard(s1, s2 string) float64 {
	return defects.CalculateTokenJaccard(s1, s2)
}

// ComputeFileSHA256 计算物理文件哈希 (复用 defects.ComputeFileSHA256 单一真实源)
func ComputeFileSHA256(filePath string) (string, error) {
	return defects.ComputeFileSHA256(filePath)
}

// ComputeCleanTokenHash 计算代码块 Token 哈希 (复用 defects.ComputeCleanTokenHash 单一真实源)
func ComputeCleanTokenHash(content string) string {
	return defects.ComputeCleanTokenHash(content)
}

// EnrichSourceAnchor 校准物理行并提取物理锚点 (复用 defects.EnrichSourceAnchor 单一真实源)
func EnrichSourceAnchor(repoRoot, filePath, lineNumber, triggerLine string) (*SourceAnchor, error) {
	return defects.EnrichSourceAnchor(repoRoot, filePath, lineNumber, triggerLine)
}


// SanitizeCategory 白名单分类吸附
func SanitizeCategory(rawCat string) string {
	s := strings.TrimSpace(rawCat)
	if s == "" {
		return "其他"
	}
	// 去除前缀序号如 "1. " 或 "1、"
	rePrefix := regexp.MustCompile(`^[0-9]+[.、\s]+`)
	s = rePrefix.ReplaceAllString(s, "")
	return s
}

// SortFindingsBySeverityDescending 按严重度权重对条目降序排序
func SortFindingsBySeverityDescending(findings []models.AnalysisFinding) []models.AnalysisFinding {
	res := make([]models.AnalysisFinding, len(findings))
	copy(res, findings)
	sort.SliceStable(res, func(i, j int) bool {
		wI := severityWeight(res[i].Severity)
		wJ := severityWeight(res[j].Severity)
		if wI != wJ {
			return wI > wJ
		}
		return res[i].FilePath < res[j].FilePath
	})
	return res
}
