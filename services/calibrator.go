package services

import (
	"code-shield/models"
	"regexp"
	"strings"
)

// CalibrateSeverityDeterministically 依据确定性规则决策树计算严重级别，剥夺大模型自由裁量权
// 决策矩阵参考 docs/agentic-scanner-architecture/02-CodeShield-下一代AI引擎架构设计.md §3.4
func CalibrateSeverityDeterministically(category string, verdict string, codeSnippet string) (string, string) {
	// 1. 条件性缺陷（如必须开启非默认宏方可触发）一律降级为 "一般"
	if verdict == models.DebateVerdictConditional || strings.EqualFold(verdict, "CONDITIONAL") {
		return "一般", "RULE_CONDITIONAL_MACRO_DOWNGRADE"
	}

	catLower := strings.ToLower(category)

	// 2. 判断是否受非默认宏隔离保护
	hasMacroGuard := checkMacroIsolationGuard(codeSnippet)

	// 3. 内存破坏类（写越界、释放后使用 UAF、栈溢出/栈写穿、堆溢出）
	if isMemoryCorruption(catLower, codeSnippet) {
		if hasMacroGuard {
			return "一般", "RULE_MEM_CORRUPTION_MACRO_GUARDED"
		}
		return "严重", "RULE_MEM_CORRUPTION_DEFAULT_REACHABLE"
	}

	// 4. 确定性崩溃/段错误/致命逻辑（空指针解引用、堆栈读越界、未捕获异常、测试无效断言）
	if isDeterministicCrash(catLower, codeSnippet) {
		if hasMacroGuard {
			return "建议", "RULE_CRASH_MACRO_GUARDED"
		}
		return "高", "RULE_CRASH_DETERMINISTIC"
	}

	// 5. 资源耗尽 / DoS / 性能隐患 / Flaky Test（未受限大内存分配、死循环、并发锁竞争）
	if isResourceExhaustionOrFlaky(catLower, codeSnippet) {
		return "一般", "RULE_RESOURCE_DOS_OR_FLAKY"
	}

	// 6. 其余架构坏味道 / 防御性缺失 / 命名与代码风格
	return "建议", "RULE_ARCH_STYLE_SUGGESTION"
}

// CalibrateFindings 批量校准 findings 列表的严重程度
func CalibrateFindings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	for i := range findings {
		verdict := findings[i].JudgeVerdict
		if verdict == "" {
			verdict = models.DebateVerdictConfirmed
		}
		sev, rule := CalibrateSeverityDeterministically(findings[i].Category, verdict, findings[i].CodeSnippet)
		findings[i].Severity = sev
		findings[i].CalibrationRule = rule
	}
	return findings
}

// isMemoryCorruption 判断是否为内存写破坏类缺陷 (P0 严重级特征)
func isMemoryCorruption(categoryLower string, snippet string) bool {
	keywords := []string{
		"write", "写越界", "栈溢出", "栈写穿", "堆溢出", "uaf",
		"use-after-free", "use after free", "释放后使用",
		"buffer overflow", "缓冲区溢出", "memory corruption", "内存破坏",
		"double free", "双重释放", "stack-buffer-overflow", "heap-buffer-overflow",
	}
	for _, kw := range keywords {
		if strings.Contains(categoryLower, kw) {
			return true
		}
	}

	// 代码特征判断（向固定数组或指针无上界写入）
	snippetLower := strings.ToLower(snippet)
	if strings.Contains(snippetLower, "buffer.data()") || strings.Contains(snippetLower, "strcpy") || strings.Contains(snippetLower, "sprintf") {
		if strings.Contains(categoryLower, "越界") || strings.Contains(categoryLower, "overflow") {
			return true
		}
	}
	return false
}

// isDeterministicCrash 判断是否为确定性崩溃/段错误类缺陷 (P1 高危级特征)
func isDeterministicCrash(categoryLower string, snippet string) bool {
	keywords := []string{
		"nullptr", "null pointer", "空指针", "sigsegv", "段错误",
		"out-of-bounds read", "out of bounds read", "读越界", "越界读",
		"cwe-125", "cwe-476", "nil pointer", "panic", "除零", "divide by zero",
		"invalid assertion", "有效性", "断言失效", "无断言", "thread-create",
	}
	for _, kw := range keywords {
		if strings.Contains(categoryLower, kw) {
			return true
		}
	}

	snippetLower := strings.ToLower(snippet)
	if strings.Contains(snippetLower, "*++it") || strings.Contains(snippetLower, "*it++") {
		return true
	}
	return false
}

// isResourceExhaustionOrFlaky 判断是否为资源耗尽/DoS/单测质量隐患 (P2 一般级特征)
func isResourceExhaustionOrFlaky(categoryLower string, snippet string) bool {
	keywords := []string{
		"dos", "oom", "bad_alloc", "内存泄漏", "memory leak",
		"resource exhaustion", "资源耗尽", "flaky", "sleep",
		"unordered-collection", "float-comparison", "浮点比较",
		"cjson", "未释放",
	}
	for _, kw := range keywords {
		if strings.Contains(categoryLower, kw) {
			return true
		}
	}
	return false
}

// checkMacroIsolationGuard 检查代码片段是否包裹在非默认开启的宏保护分支中
func checkMacroIsolationGuard(codeSnippet string) bool {
	if codeSnippet == "" {
		return false
	}
	reMacro := regexp.MustCompile(`(?i)#\s*(if|ifdef|ifndef)\s+([A-Za-z0-9_]+)`)
	matches := reMacro.FindAllStringSubmatch(codeSnippet, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			macroName := strings.ToUpper(m[2])
			// 常见非默认/实验性宏标识
			if strings.Contains(macroName, "EXPERIMENTAL") ||
				strings.Contains(macroName, "GRISU") ||
				strings.Contains(macroName, "LEGACY") ||
				strings.Contains(macroName, "DEBUG") ||
				strings.Contains(macroName, "CUSTOM") ||
				strings.Contains(macroName, "OPTIONAL") {
				return true
			}
		}
	}
	return false
}
