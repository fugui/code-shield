package defects

import (
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

// SourceAnchor 确定性物理源码锚点（缺陷的绝对物理身份证）
type SourceAnchor struct {
	NormalizedPath  string `json:"normalized_path"`  // 相对仓库根目录的正斜杠小写路径 (e.g. "src/util/buffer.cpp")
	NormalizedScope string `json:"normalized_scope"` // 规范化后的物理函数/方法签名，去除外部命名空间 (e.g. "BufferPool::Allocate")
	PhysicalToken   string `json:"physical_token"`   // 从物理文件中读取并清洗的单行真实核心代码语句
	StartLine       int    `json:"start_line"`       // 物理文件中的真实绝对起始行
	EndLine         int    `json:"end_line"`         // 物理文件中的真实绝对结束行
	ScopeBodyHash   string `json:"scope_body_hash"`  // 所在函数/作用域代码块的 SHA-256 (用于细粒度代码修改守卫)
}

// CleanSourceToken 对代码行进行 Token 级去噪清洗 (去除多语言注释、空白符、引号、分号并转小写)
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

	// 3. 移除单双引号与反引号及末尾分号
	clean = strings.ReplaceAll(clean, "\"", "")
	clean = strings.ReplaceAll(clean, "'", "")
	clean = strings.ReplaceAll(clean, "`", "")
	clean = strings.TrimSuffix(clean, ";")

	return strings.ToLower(clean)
}

// NormalizeScopeSymbol 规范化作用域符号，去除外层命名空间与 lambda 差异
// 规则：剥离外层 namespace，统一保留最内层核心 Class::Method (取最后 2 段)；规范化 lambda 为 <lambda>
func NormalizeScopeSymbol(rawScope string) string {
	trimmed := strings.TrimSpace(rawScope)
	if trimmed == "" {
		return ""
	}

	// 1. 处理多函数合并的情况 (如 "GetTriggerDelay / GetTriggerFreq")，分段规范化后排序拼接
	parts := strings.Split(trimmed, " / ")
	var normalized []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}

		// 统一将各类 lambda / operator() 描述规范化为占位符，防止被模板参数剥离误杀
		if strings.Contains(p, "lambda") || strings.Contains(p, "operator()") {
			p = regexp.MustCompile(`(operator\(\)[^:]*|<lambda[^>]*>|<lambda\(\)>)`).ReplaceAllString(p, "__LAMBDA__")
		}

		// 剥离 C++ 模板参数 <T>
		p = regexp.MustCompile(`<.*?>`).ReplaceAllString(p, "")

		// 恢复为标准 <lambda>
		p = strings.ReplaceAll(p, "__LAMBDA__", "<lambda>")

		// 按 C++ / 语言分隔符拆分，取最后至多 2 段
		segs := strings.Split(p, "::")
		var validSegs []string
		for _, seg := range segs {
			s := strings.TrimSpace(seg)
			if s != "" {
				validSegs = append(validSegs, s)
			}
		}

		if len(validSegs) >= 2 {
			normalized = append(normalized, validSegs[len(validSegs)-2]+"::"+validSegs[len(validSegs)-1])
		} else if len(validSegs) == 1 {
			normalized = append(normalized, validSegs[0])
		}
	}

	if len(normalized) == 0 {
		return ""
	}

	sort.Strings(normalized)
	return strings.Join(normalized, " / ")
}

// ParseLineNumberRange 解析 "10-20" 或 "15" 格式的行号，返回起始行与结束行
func ParseLineNumberRange(rawLine string) (int, int) {
	raw := strings.TrimSpace(rawLine)
	if raw == "" {
		return 0, 0
	}
	parts := strings.Split(raw, "-")
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0
	}
	if len(parts) > 1 {
		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err == nil && end >= start {
			return start, end
		}
	}
	return start, start
}

// ComputeFileSHA256 计算物理文件的 SHA-256 快照哈希
func ComputeFileSHA256(filePath string) (string, error) {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:]), nil
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

// LocateTriggerNearby 在 targetLine 前后指定窗口内滑动寻找最匹配 cleanTrigger 的物理行
func LocateTriggerNearby(lines []string, cleanTrigger string, targetLine int, windowSize int) int {
	if cleanTrigger == "" || len(lines) == 0 {
		return targetLine
	}
	total := len(lines)
	if targetLine <= 0 {
		targetLine = 1
	}

	start := targetLine - windowSize
	if start < 1 {
		start = 1
	}
	end := targetLine + windowSize
	if end > total {
		end = total
	}

	// 1. 优先寻找 Token 完全相等的物理行 (由近及远辐射查找)
	for delta := 0; delta <= windowSize; delta++ {
		down := targetLine + delta
		if down <= end && down >= start {
			if CleanSourceToken(lines[down-1]) == cleanTrigger {
				return down
			}
		}
		up := targetLine - delta
		if up >= start && up <= end && up != down {
			if CleanSourceToken(lines[up-1]) == cleanTrigger {
				return up
			}
		}
	}

	// 2. 次选包含关系的物理行
	for delta := 0; delta <= windowSize; delta++ {
		down := targetLine + delta
		if down <= end && down >= start {
			tok := CleanSourceToken(lines[down-1])
			if tok != "" && (strings.Contains(tok, cleanTrigger) || strings.Contains(cleanTrigger, tok)) {
				return down
			}
		}
		up := targetLine - delta
		if up >= start && up <= end && up != down {
			tok := CleanSourceToken(lines[up-1])
			if tok != "" && (strings.Contains(tok, cleanTrigger) || strings.Contains(cleanTrigger, tok)) {
				return up
			}
		}
	}

	return targetLine
}

// LocateTriggerInLines 在整篇文件中模糊反查 cleanTrigger 所在真实行号 (解决行号为空的场景)
func LocateTriggerInLines(lines []string, cleanTrigger string) int {
	if cleanTrigger == "" || len(lines) == 0 {
		return 1
	}

	// 1. 全文精确匹配
	for i, line := range lines {
		if CleanSourceToken(line) == cleanTrigger {
			return i + 1
		}
	}

	// 2. 全文包含匹配
	bestLine := 1
	for i, line := range lines {
		tok := CleanSourceToken(line)
		if tok != "" && (strings.Contains(tok, cleanTrigger) || strings.Contains(cleanTrigger, tok)) {
			bestLine = i + 1
			break
		}
	}

	return bestLine
}

// ExtractScopeAndBodyFromLines 从目标行向上逆向扫描提取物理函数作用域签名及函数体代码
func ExtractScopeAndBodyFromLines(filePath string, lines []string, targetLine int) (string, string) {
	if targetLine <= 0 || len(lines) == 0 {
		return "", ""
	}
	if targetLine > len(lines) {
		targetLine = len(lines)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	var scopeSign string
	scopeStart := 1

	// 从目标行向上逆向扫描最多 200 行寻找函数声明
	scanStart := targetLine
	limit := targetLine - 200
	if limit < 1 {
		limit = 1
	}

	reFuncGo := regexp.MustCompile(`func\s+(\([^)]+\)\s+)?([A-Za-z0-9_]+)`)
	reFuncCpp := regexp.MustCompile(`(?:[A-Za-z0-9_]+::)*[A-Za-z0-9_]+\s*\([^;)]*\)\s*(?:const)?\s*\{?`)
	reFuncJava := regexp.MustCompile(`(?:public|protected|private|static|\s)+[\w<>\[\]]+\s+([A-Za-z0-9_]+)\s*\([^)]*\)`)
	reFuncPy := regexp.MustCompile(`(?:def|class)\s+([A-Za-z0-9_]+)`)

scanLoop:
	for i := scanStart; i >= limit; i-- {
		line := strings.TrimSpace(lines[i-1])
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}

		switch ext {
		case ".go":
			if m := reFuncGo.FindStringSubmatch(line); len(m) >= 3 {
				scopeSign = m[2]
				if m[1] != "" {
					scopeSign = strings.TrimSpace(m[1]) + "." + m[2]
				}
				scopeStart = i
				break scanLoop
			}
		case ".py":
			if m := reFuncPy.FindStringSubmatch(line); len(m) >= 2 {
				scopeSign = m[1]
				scopeStart = i
				break scanLoop
			}
		case ".java", ".kt":
			if m := reFuncJava.FindStringSubmatch(line); len(m) >= 2 {
				scopeSign = m[1]
				scopeStart = i
				break scanLoop
			}
		default:
			// C / C++
			if m := reFuncCpp.FindString(line); m != "" && !strings.HasPrefix(line, "if") && !strings.HasPrefix(line, "while") && !strings.HasPrefix(line, "for") && !strings.HasPrefix(line, "switch") {
				idx := strings.Index(m, "(")
				if idx != -1 {
					scopeSign = strings.TrimSpace(m[:idx])
				} else {
					scopeSign = strings.TrimSpace(m)
				}
				scopeStart = i
				break scanLoop
			}
		}
	}

	// 提取函数体（从 scopeStart 向下提取至多 150 行）
	scopeEnd := scopeStart + 150
	if scopeEnd > len(lines) {
		scopeEnd = len(lines)
	}
	var bodyBuilder strings.Builder
	for k := scopeStart; k <= scopeEnd; k++ {
		bodyBuilder.WriteString(lines[k-1])
		bodyBuilder.WriteByte('\n')
	}

	return scopeSign, bodyBuilder.String()
}

// EnrichSourceAnchor 从磁盘物理源文件中提取确定性特征与物理锚点
func EnrichSourceAnchor(repoRoot string, filePath string, rawLine string, rawTrigger string) (*SourceAnchor, error) {
	normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(filePath)))
	fullPath := filepath.Join(repoRoot, normPath)

	contentBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read source file error (%s): %w", fullPath, err)
	}
	lines := strings.Split(string(contentBytes), "\n")
	totalLines := len(lines)

	// 1. 行号解析与双重校准
	startLine, endLine := ParseLineNumberRange(rawLine)
	cleanTrigger := CleanSourceToken(rawTrigger)
	targetLine := startLine

	if targetLine > 0 && targetLine <= totalLines {
		currentLineToken := CleanSourceToken(lines[targetLine-1])
		isCurrentLineMatched := currentLineToken != "" && (currentLineToken == cleanTrigger ||
			strings.Contains(currentLineToken, cleanTrigger) ||
			strings.Contains(cleanTrigger, currentLineToken))

		if cleanTrigger != "" && !isCurrentLineMatched {
			// 行号偏差！在前后 ±15 行窗口内精确对齐校准
			calibratedLine := LocateTriggerNearby(lines, cleanTrigger, targetLine, 15)
			if calibratedLine > 0 {
				targetLine = calibratedLine
				startLine = calibratedLine
				endLine = calibratedLine
			}
		}
	} else if cleanTrigger != "" {
		// 行号为空或越界，全文反查
		targetLine = LocateTriggerInLines(lines, cleanTrigger)
		startLine = targetLine
		endLine = targetLine
	}

	if targetLine <= 0 {
		targetLine = 1
		startLine = 1
		endLine = 1
	}

	// 2. 从校准后的物理行提取真实物理代码 Token
	physicalLine := lines[targetLine-1]
	cleanPhysicalToken := CleanSourceToken(physicalLine)
	if (cleanPhysicalToken == "" || cleanPhysicalToken == "{" || cleanPhysicalToken == "}") && targetLine < totalLines {
		cleanPhysicalToken = CleanSourceToken(lines[targetLine])
	}

	// 3. 逆向提取作用域并规范化命名空间
	rawScope, scopeBody := ExtractScopeAndBodyFromLines(normPath, lines, targetLine)
	normScope := NormalizeScopeSymbol(rawScope)
	if normScope == "" {
		normScope = filepath.Base(normPath)
	}

	// 4. 计算所在函数代码块 Hash (用于细粒度物理修改守卫)
	scopeBodyHash := ""
	if scopeBody != "" {
		h := sha256.Sum256([]byte(CleanSourceToken(scopeBody)))
		scopeBodyHash = hex.EncodeToString(h[:])
	}

	return &SourceAnchor{
		NormalizedPath:  normPath,
		NormalizedScope: normScope,
		PhysicalToken:   cleanPhysicalToken,
		StartLine:       startLine,
		EndLine:         endLine,
		ScopeBodyHash:   scopeBodyHash,
	}, nil
}
