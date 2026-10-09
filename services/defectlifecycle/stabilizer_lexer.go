package defectlifecycle

import (
	"regexp"
	"strings"
)

// AnchorConfidence 定桩置信度等级
type AnchorConfidence string

const (
	ConfidenceHigh   AnchorConfidence = "HIGH"   // 实体唯一确定且与 trigger/prompt 完全一致
	ConfidenceMedium AnchorConfidence = "MEDIUM" // 实体由物理语法优先级或启发式就近选出
	ConfidenceLow    AnchorConfidence = "LOW"    // 实体未提取到，依赖弹性结构匹配
)

// C/C++ 及通用关键字与常量黑名单（严禁作为受体标识符）
var keywordBlacklist = map[string]bool{
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "default": true, "break": true, "continue": true,
	"return": true, "goto": true, "try": true, "catch": true, "throw": true,
	"const": true, "static": true, "unsigned": true, "signed": true, "volatile": true,
	"extern": true, "inline": true, "virtual": true, "override": true, "final": true,
	"auto": true, "void": true, "int": true, "char": true, "short": true, "long": true,
	"float": true, "double": true, "bool": true, "size_t": true, "uint32_t": true, "uint64_t": true,
	"int32_t": true, "int64_t": true, "nullptr": true, "null": true, "NULL": true,
	"true": true, "false": true, "TRUE": true, "FALSE": true, "this": true, "new": true, "delete": true,
	"public": true, "protected": true, "private": true, "class": true, "struct": true, "namespace": true,
	"using": true, "typedef": true, "template": true, "typename": true,
}

// 标准安全 Sink 列表
var standardSinks = []string{
	"sqlite3_exec", "system", "popen", "execve", "execl", "execvp",
	"strcpy", "strcat", "sprintf", "vsprintf", "gets", "scanf",
	"memcpy", "memmove", "malloc", "free", "realloc",
}

var reMacroCall = regexp.MustCompile(`\b([A-Z0-9_]{3,})\s*\(\s*([A-Za-z0-9_]+)`)
var reArrowDot = regexp.MustCompile(`([A-Za-z0-9_]+)\s*(?:->|\.)`)
var reIdentifier = regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\b`)
var reFunctionCall = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)

// isKeywordOrLiteral 检查是否为编程语言关键字或字面量
func isKeywordOrLiteral(ident string) bool {
	return keywordBlacklist[ident] || keywordBlacklist[strings.ToLower(ident)]
}

// hasWordBoundaryToken 检查 cleanLine 中是否存在具有独立单词边界的核心符号
func hasWordBoundaryToken(line string, token string) bool {
	if token == "" {
		return false
	}
	pattern := `\b` + regexp.QuoteMeta(token) + `\b`
	matched, _ := regexp.MatchString(pattern, line)
	return matched
}

// ReconstructStatementWindow 以 targetLine 为中心，在 [targetLine-3, targetLine+3] 窗口内重构多行物理逻辑语句
func ReconstructStatementWindow(lines []string, targetLine int) string {
	if len(lines) == 0 {
		return ""
	}
	total := len(lines)
	if targetLine <= 0 {
		targetLine = 1
	} else if targetLine > total {
		targetLine = total
	}

	start := targetLine - 3
	if start < 1 {
		start = 1
	}
	end := targetLine + 3
	if end > total {
		end = total
	}

	// 向前寻找语句起始 (遇到分号或左花括号停止向前，但包含当前句内容)
	actualStart := targetLine
	for i := targetLine - 1; i >= start; i-- {
		prevLine := strings.TrimSpace(lines[i-1])
		if strings.HasSuffix(prevLine, ";") || strings.HasSuffix(prevLine, "{") || strings.HasSuffix(prevLine, "}") {
			break
		}
		actualStart = i
	}

	// 向后寻找语句结尾 (遇到分号或花括号停止向后)
	actualEnd := targetLine
	for i := targetLine; i <= end; i++ {
		curLine := strings.TrimSpace(lines[i-1])
		actualEnd = i
		if strings.HasSuffix(curLine, ";") || strings.HasSuffix(curLine, "{") || strings.HasSuffix(curLine, "}") {
			break
		}
	}

	var builder strings.Builder
	for i := actualStart; i <= actualEnd; i++ {
		l := strings.TrimSpace(lines[i-1])
		if l != "" {
			if builder.Len() > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(l)
		}
	}

	res := builder.String()
	if res == "" && targetLine <= total {
		return strings.TrimSpace(lines[targetLine-1])
	}
	return res
}

// extractLeftmostRoot 提取复合链式表达式最左侧根对象 (例如 pSession->GetContext()->req -> pSession)
func extractLeftmostRoot(stmt string) string {
	matches := reArrowDot.FindAllStringSubmatch(stmt, -1)
	for _, m := range matches {
		if len(m) > 1 {
			cand := m[1]
			if !isKeywordOrLiteral(cand) {
				return cand
			}
		}
	}
	return ""
}

// unwrapMacroCall 穿透全大写防御宏，解包实参变量 (如 SAFE_DELETE(pPtr) -> pPtr)
func unwrapMacroCall(stmt string) string {
	matches := reMacroCall.FindAllStringSubmatch(stmt, -1)
	for _, m := range matches {
		if len(m) > 2 {
			macroName := m[1]
			argName := m[2]
			// 如果宏名称全部由大写字母、数字或下划线组成，且不是普通关键字
			if macroName == strings.ToUpper(macroName) && !isKeywordOrLiteral(argName) {
				return argName
			}
		}
	}
	return ""
}

// extractStandardSink 提取语句中的高危标准 Sink 函数
func extractStandardSink(stmt string) string {
	for _, sink := range standardSinks {
		if hasWordBoundaryToken(stmt, sink) {
			return "SINK:" + sink
		}
	}
	return ""
}

// extractCalleeIdentifier 提取语句中首个被调函数名作为合成受体
func extractCalleeIdentifier(stmt string, prefix string) string {
	matches := reFunctionCall.FindAllStringSubmatch(stmt, -1)
	for _, m := range matches {
		if len(m) > 1 {
			fn := m[1]
			if !isKeywordOrLiteral(fn) {
				return prefix + ":" + fn
			}
		}
	}
	return ""
}

// extractLexicalCandidates 纯 Go 轻量词法抽取物理候选变量标识符
func extractLexicalCandidates(stmt string, classMajor string) []string {
	var candidates []string
	seen := make(map[string]bool)

	addCandidate := func(cand string) {
		cand = strings.TrimSpace(cand)
		if cand != "" && !seen[cand] && !isKeywordOrLiteral(cand) {
			seen[cand] = true
			candidates = append(candidates, cand)
		}
	}

	// 1. 优先检查宏穿透 (SAFE_DELETE(ptr) -> ptr)
	if unwrapped := unwrapMacroCall(stmt); unwrapped != "" {
		addCandidate(unwrapped)
	}

	// 2. 检查链式调用的最左侧根实体 (pSession->GetContext() -> pSession)
	if root := extractLeftmostRoot(stmt); root != "" {
		addCandidate(root)
	}

	// 3. 通用标识符扫描
	matches := reIdentifier.FindAllString(stmt, -1)
	for _, id := range matches {
		addCandidate(id)
	}

	return candidates
}

// ExtractPhysicalTargetEntity 纯 Go 确定性受体抽取 (零 CGO，执行耗时 < 0.05ms)
func ExtractPhysicalTargetEntity(physicalSnippet string, promptTargetSymbol string, classMajor string) (string, AnchorConfidence) {
	cleanLine := strings.TrimSpace(physicalSnippet)
	if cleanLine == "" {
		return "", ConfidenceLow
	}

	normPromptSymbol := strings.TrimSpace(promptTargetSymbol)

	// 1. 广义合成受体专项处理 (SINK / LOCK / SIGNAL / CONTROL)
	if strings.HasPrefix(normPromptSymbol, "SINK:") ||
		strings.HasPrefix(normPromptSymbol, "LOCK:") ||
		strings.HasPrefix(normPromptSymbol, "SHARED:") ||
		strings.HasPrefix(normPromptSymbol, "SIGNAL:") ||
		strings.HasPrefix(normPromptSymbol, "CONTROL:") {
		parts := strings.SplitN(normPromptSymbol, ":", 2)
		if len(parts) == 2 {
			coreSymbol := parts[1]
			if hasWordBoundaryToken(cleanLine, coreSymbol) {
				return normPromptSymbol, ConfidenceHigh
			}
			return normPromptSymbol, ConfidenceMedium
		}
	}

	// 根据受控大类自动派生合成受体
	switch classMajor {
	case "SEC_INJECT":
		if sink := extractStandardSink(cleanLine); sink != "" {
			return sink, ConfidenceHigh
		}
		if callee := extractCalleeIdentifier(cleanLine, "SINK"); callee != "" {
			return callee, ConfidenceMedium
		}
	case "DEADLOCK", "DATA_RACE", "CONCUR":
		// 优先识别带有 mtx, lock, mutex 标识的变量
		candidates := extractLexicalCandidates(cleanLine, classMajor)
		for _, cand := range candidates {
			lower := strings.ToLower(cand)
			if strings.Contains(lower, "mtx") || strings.Contains(lower, "mutex") || strings.Contains(lower, "lock") {
				return "LOCK:" + cand, ConfidenceHigh
			}
		}
	case "SIGNAL_SAFE":
		if callee := extractCalleeIdentifier(cleanLine, "SIGNAL"); callee != "" {
			return callee, ConfidenceHigh
		}
	case "LOGIC_RISK":
		if strings.Contains(cleanLine, "while") || strings.Contains(cleanLine, "for") {
			return "CONTROL:loop", ConfidenceMedium
		}
	}

	// 2. 纯 Go 词法扫描物理候选标识符
	candidates := extractLexicalCandidates(cleanLine, classMajor)
	if len(candidates) == 0 {
		return "", ConfidenceLow
	}

	// 3. Prompt target_symbol 契约语义判歧 (Tie-Breaker)
	if normPromptSymbol != "" && !isKeywordOrLiteral(normPromptSymbol) {
		for _, cand := range candidates {
			if cand == normPromptSymbol {
				// 物理源码确实存在该变量，且与大模型一致：HIGH 置信度
				return cand, ConfidenceHigh
			}
		}
	}

	// 4. 降级安全网：确定性选用物理语法优先级首选受体 (同一代码行绝对恒定，抗 LLM 抖动)
	return candidates[0], ConfidenceMedium
}
