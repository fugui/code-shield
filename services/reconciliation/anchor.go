package reconciliation

import (
	"code-shield/models"
	"code-shield/services/defects"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CanonicalizeRelativePath 统一规范化代码仓相对路径 (SSOT)
func CanonicalizeRelativePath(filePath, repoRoot string) string {
	clean := filepath.ToSlash(strings.TrimSpace(filePath))
	if clean == "" {
		return ""
	}

	// 1. 如果是绝对路径，且以 repoRoot 为前缀，直接剥离
	if filepath.IsAbs(clean) && repoRoot != "" {
		cleanRepo := filepath.ToSlash(filepath.Clean(repoRoot))
		if strings.HasPrefix(clean, cleanRepo) {
			clean = strings.TrimPrefix(clean, cleanRepo)
			clean = strings.TrimPrefix(clean, "/")
		}
	}

	// 2. 清除开头的 ./ 或 /
	for strings.HasPrefix(clean, "./") || strings.HasPrefix(clean, "/") {
		clean = strings.TrimPrefix(clean, "./")
		clean = strings.TrimPrefix(clean, "/")
	}

	// 3. 若提供 repoRoot，检查当前 clean 路径在 repoRoot 下是否存在
	if repoRoot != "" {
		if _, err := os.Stat(filepath.Join(repoRoot, clean)); err == nil {
			return filepath.Clean(clean)
		}

		repoBase := filepath.Base(filepath.Clean(repoRoot))
		if repoBase != "" && repoBase != "." && repoBase != "/" {
			idx := strings.Index(clean, repoBase+"/")
			if idx != -1 {
				candidate := clean[idx+len(repoBase)+1:]
				if _, err := os.Stat(filepath.Join(repoRoot, candidate)); err == nil {
					return filepath.Clean(candidate)
				}
			}
		}
	}

	// 4. 通用启发式：如果包含常见的源码顶级目录名（如 include/, src/, lib/, support/, doc/ 等）
	commonTopDirs := []string{"include/", "src/", "lib/", "support/", "doc/", "docs/", "test/", "tests/", "pkg/", "cmd/"}
	for _, topDir := range commonTopDirs {
		idx := strings.Index(clean, topDir)
		if idx > 0 {
			candidate := clean[idx:]
			if repoRoot != "" {
				if _, err := os.Stat(filepath.Join(repoRoot, candidate)); err == nil {
					return filepath.Clean(candidate)
				}
			}
		}
	}

	// 5. 若未在磁盘上命中（离线测试或文件已改），按 repoBase 或顶级目录截断
	if repoRoot != "" {
		repoBase := filepath.Base(filepath.Clean(repoRoot))
		if repoBase != "" && repoBase != "." && repoBase != "/" {
			idx := strings.Index(clean, repoBase+"/")
			if idx != -1 {
				return filepath.Clean(clean[idx+len(repoBase)+1:])
			}
		}
	}
	for _, topDir := range commonTopDirs {
		idx := strings.Index(clean, topDir)
		if idx > 0 {
			return filepath.Clean(clean[idx:])
		}
	}

	return filepath.Clean(clean)
}

// CalculateSnippetOverlap 计算两个代码块 Token 集合重叠率 [0.0, 1.0]
func CalculateSnippetOverlap(s1, s2 string) float64 {
	t1 := CleanSourceToken(s1)
	t2 := CleanSourceToken(s2)
	if t1 == "" || t2 == "" {
		return 0.0
	}
	if t1 == t2 {
		return 1.0
	}
	return CalculateTokenJaccard(t1, t2)
}

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
