package reconciliation

import (
	"code-shield/models"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SourceAnchor 物理源码锚点
type SourceAnchor struct {
	NormalizedPath  string
	NormalizedScope string
	PhysicalToken   string
	StartLine       int
	EndLine         int
	ScopeBodyHash   string
}

// CleanSourceToken 对代码行进行 Token 级去噪清洗 (去除注释、空白符、引号、分号并转小写)
func CleanSourceToken(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	// 1. 移除 C/C++/Go/Java/Python/Shell 单行与多行注释
	reComments := regexp.MustCompile(`(//.*?$|/\*.*?\*/|#.*?$)`)
	clean := reComments.ReplaceAllString(trimmed, "")

	// 2. 移除所有空白符与换行符
	reWhitespace := regexp.MustCompile(`\s+`)
	clean = reWhitespace.ReplaceAllString(clean, "")

	// 3. 移除单双引号与末尾分号
	clean = strings.ReplaceAll(clean, "\"", "")
	clean = strings.ReplaceAll(clean, "'", "")
	clean = strings.ReplaceAll(clean, "`", "")
	clean = strings.TrimSuffix(clean, ";")

	return strings.ToLower(clean)
}

// NormalizeScopeSymbol 规范化作用域符号 (剥离外层顶级命名空间、规范化 Lambda 与泛型)
func NormalizeScopeSymbol(scope string) string {
	s := strings.TrimSpace(scope)
	if s == "" {
		return ""
	}

	// 1. 移除 lambda 命名抖动 (如 ::lambda_123 或 [this](...) 替换为规范占位符)
	reLambda := regexp.MustCompile(`::lambda_[0-9a-zA-Z_]+|\[.*?\]\(.*?\)`)
	s = reLambda.ReplaceAllString(s, "::<lambda>")

	// 2. 剥离外层项目命名空间 (如 PROJ_CORE_COMMON::OpcuaMonitor<T>::SubscribeNodes -> OpcuaMonitor<T>::SubscribeNodes)
	parts := strings.Split(s, "::")
	if len(parts) > 2 {
		firstPart := parts[0]
		if strings.HasSuffix(firstPart, "_COMMON") || strings.HasPrefix(firstPart, "PROJ_") || strings.HasPrefix(firstPart, "XY_") || strings.Contains(firstPart, "MODULE") {
			s = strings.Join(parts[1:], "::")
		}
	}

	return s
}

// ParseLineNumberRange 解析 "120-135" 或 "120" 为 startLine 和 endLine
func ParseLineNumberRange(lineStr string) (int, int) {
	lineStr = strings.TrimSpace(lineStr)
	if lineStr == "" {
		return 0, 0
	}

	if strings.Contains(lineStr, "-") {
		parts := strings.Split(lineStr, "-")
		s, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		e, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		if e < s {
			e = s
		}
		return s, e
	}

	n, _ := strconv.Atoi(lineStr)
	return n, n
}

// CalculateDefectFingerprint 计算 L1 物理强指纹
// SHA256("repo:%d|task:%d|path:%s|scope:%s|token:%s")
func CalculateDefectFingerprint(repoID uint, taskTypeID uint, filePath string, triggerLine string, scopeSymbol string) string {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	normScope := NormalizeScopeSymbol(scopeSymbol)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}
	normTrigger := CleanSourceToken(triggerLine)

	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s|token:%s",
		repoID, taskTypeID, normPath, normScope, normTrigger)

	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// CalculateWeakScopeFingerprint 计算 L2 弱作用域指纹
func CalculateWeakScopeFingerprint(repoID uint, taskTypeID uint, filePath string, scopeSymbol string) string {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	normScope := NormalizeScopeSymbol(scopeSymbol)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}

	rawKey := fmt.Sprintf("repo:%d|task:%d|path:%s|scope:%s",
		repoID, taskTypeID, normPath, normScope)

	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// CalculateTokenJaccard 计算 2-gram Jaccard 相似度 [0.0, 1.0]
func CalculateTokenJaccard(s1, s2 string) float64 {
	t1 := CleanSourceToken(s1)
	t2 := CleanSourceToken(s2)
	if t1 == t2 {
		return 1.0
	}
	if len(t1) == 0 || len(t2) == 0 {
		return 0.0
	}

	grams1 := make(map[string]bool)
	grams2 := make(map[string]bool)

	if len(t1) < 2 {
		grams1[t1] = true
	} else {
		for i := 0; i < len(t1)-1; i++ {
			grams1[t1[i:i+2]] = true
		}
	}

	if len(t2) < 2 {
		grams2[t2] = true
	} else {
		for i := 0; i < len(t2)-1; i++ {
			grams2[t2[i:i+2]] = true
		}
	}

	intersection := 0
	for g := range grams1 {
		if grams2[g] {
			intersection++
		}
	}

	union := len(grams1) + len(grams2) - intersection
	if union <= 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// ComputeFileSHA256 计算物理文件哈希
func ComputeFileSHA256(filePath string) (string, error) {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(bytes)
	return hex.EncodeToString(h[:]), nil
}

// ComputeCleanTokenHash 计算代码块 Token 哈希
func ComputeCleanTokenHash(content string) string {
	clean := CleanSourceToken(content)
	if clean == "" {
		return ""
	}
	h := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(h[:])
}

// EnrichSourceAnchor 校准物理行并提取物理锚点
func EnrichSourceAnchor(repoRoot, filePath, lineNumber, triggerLine string) (*SourceAnchor, error) {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	cleanTrigger := CleanSourceToken(triggerLine)
	startLine, endLine := ParseLineNumberRange(lineNumber)

	anchor := &SourceAnchor{
		NormalizedPath: normPath,
		PhysicalToken:  cleanTrigger,
		StartLine:      startLine,
		EndLine:        endLine,
	}

	if repoRoot == "" {
		return anchor, nil
	}

	fullPath := filepath.Join(repoRoot, normPath)
	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return anchor, err
	}

	lines := strings.Split(string(contentBytes), "\n")
	totalLines := len(lines)
	if totalLines == 0 {
		return anchor, nil
	}

	// 1. 若给定了大致行号且存在 cleanTrigger，使用局部滑动窗口 ±15 行精确定位
	bestLine := startLine
	if cleanTrigger != "" && startLine > 0 && startLine <= totalLines {
		windowStart := startLine - 15
		if windowStart < 1 {
			windowStart = 1
		}
		windowEnd := startLine + 15
		if windowEnd > totalLines {
			windowEnd = totalLines
		}

		maxSim := 0.0
		for i := windowStart; i <= windowEnd; i++ {
			lineClean := CleanSourceToken(lines[i-1])
			sim := CalculateTokenJaccard(cleanTrigger, lineClean)
			if sim > maxSim {
				maxSim = sim
				bestLine = i
			}
		}

		if maxSim >= 0.75 {
			anchor.StartLine = bestLine
			if anchor.EndLine < anchor.StartLine {
				anchor.EndLine = anchor.StartLine
			}
			anchor.PhysicalToken = CleanSourceToken(lines[bestLine-1])
		}
	} else if cleanTrigger != "" && (startLine <= 0 || startLine > totalLines) {
		// 全文扫描定位最相似行
		maxSim := 0.0
		bestIdx := 0
		for i, l := range lines {
			lineClean := CleanSourceToken(l)
			sim := CalculateTokenJaccard(cleanTrigger, lineClean)
			if sim > maxSim {
				maxSim = sim
				bestIdx = i + 1
			}
		}
		if maxSim >= 0.85 {
			anchor.StartLine = bestIdx
			anchor.EndLine = bestIdx
			anchor.PhysicalToken = CleanSourceToken(lines[bestIdx-1])
		}
	}

	// 2. 向上扫描推断最近的作用域符号与代码块
	scope, scopeBody := extractScopeAndBody(normPath, lines, anchor.StartLine)
	anchor.NormalizedScope = NormalizeScopeSymbol(scope)
	if scopeBody != "" {
		anchor.ScopeBodyHash = ComputeCleanTokenHash(scopeBody)
	}

	return anchor, nil
}

// extractScopeAndBody 向上扫描查找所属函数签名与主体
func extractScopeAndBody(normPath string, lines []string, targetLine int) (string, string) {
	if targetLine <= 0 || targetLine > len(lines) {
		return filepath.Base(normPath), ""
	}

	// 向上回溯至多 150 行寻找函数声明
	scanStart := targetLine - 1
	limit := scanStart - 150
	if limit < 0 {
		limit = 0
	}

	reFunc := regexp.MustCompile(`^\s*(?:[\w:*&<>]+\s+)+([A-Za-z0-9_~]+(?:::[A-Za-z0-9_~]+)*)\s*\([^;]*$`)
	reGo := regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s+)?([A-Za-z0-9_]+)\s*\(`)
	rePy := regexp.MustCompile(`^\s*def\s+([A-Za-z0-9_]+)\s*\(`)

	for i := scanStart; i >= limit; i-- {
		l := lines[i]
		if m := reGo.FindStringSubmatch(l); len(m) >= 2 {
			return m[1], collectBody(lines, i)
		}
		if m := rePy.FindStringSubmatch(l); len(m) >= 2 {
			return m[1], collectBody(lines, i)
		}
		if m := reFunc.FindStringSubmatch(l); len(m) >= 2 {
			return m[1], collectBody(lines, i)
		}
	}

	return filepath.Base(normPath), ""
}

func collectBody(lines []string, startIdx int) string {
	endIdx := startIdx + 80
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	return strings.Join(lines[startIdx:endIdx], "\n")
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
