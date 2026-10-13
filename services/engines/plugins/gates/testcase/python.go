package testcase

import (
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

var (
	pyStdAssertPattern        = regexp.MustCompile(`(?m)^\s*assert\s+|self\.assert|pytest\.raises|mocker\.`)
	pyStrongAssertPattern     = regexp.MustCompile(`(?m)^\s*assert\s+.+?==|\bself\.assertEqual|\bpytest\.raises|\b(assert_called_once_with|assert_called_with|assert_has_calls)\b`)
	pyWeakAssertPattern       = regexp.MustCompile(`(?m)^\s*assert\s+(?:[a-zA-Z0-9_.]+\s+is\s+not\s+None|[a-zA-Z0-9_.]+\s*$|not\s+[a-zA-Z0-9_.]+\s*$)|\bself\.assertIsNotNone|\bself\.assertTrue`)
	pyMockAssertPattern       = regexp.MustCompile(`\b(assert_called|assert_called_once|assert_called_with|assert_called_once_with|assert_any_call|assert_has_calls|assert_not_called)\b`)
	pyMockTypoPattern         = regexp.MustCompile(`\b(asser_called|assert_caled|assert_called_wth|assert_calld)\b`)
	pyLiteralTautologyPattern = regexp.MustCompile(`\bassert\s+(True|1|not\s+False|\([Tt]rue\))\b`)
	pyHelperPattern           = regexp.MustCompile(`\b(verify_[A-Za-z0-9_]*|check_[A-Za-z0-9_]*|assert_[A-Za-z0-9_]*|validate_[A-Za-z0-9_]*)\s*\(`)
	pyPrintPattern            = regexp.MustCompile(`(?m)^\s*(print\s*\(|logging\.|logger\.)[^)]*\)?\s*$`)
	pyDocstringPattern        = regexp.MustCompile(`(?s)"""[\s\S]*?"""|'''[\s\S]*?'''`)
	pySingleCommentPattern    = regexp.MustCompile(`(?m)#.*$`)
	pyUnguardedReturnPattern  = regexp.MustCompile(`(?m)^\s*if\s+.+:\s*\n\s*return\b`)
	pySwallowedExceptPattern  = regexp.MustCompile(`(?m)^\s*except(?:\s+[^:]*)?:\s*\n\s*(?:pass|\.\.\.|logging\.[^;\n]+|logger\.[^;\n]+)\s*$`)
)

func hasPyUnguardedReturn(code string) bool {
	matches := pyUnguardedReturnPattern.FindAllString(code, -1)
	for _, m := range matches {
		if !strings.Contains(m, "pytest.fail") && !strings.Contains(m, "self.fail") {
			return true
		}
	}
	return false
}

func hasPySwallowedExcept(code string) bool {
	return pySwallowedExceptPattern.MatchString(code)
}

// PythonLinterRadar Python 特化轻量雷达探针
type PythonLinterRadar struct{}

func (r *PythonLinterRadar) CanHandle(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".py"
}

func (r *PythonLinterRadar) InspectUnit(codesPath string, unit coverage.PlanUnit) plugins.RadarResult {
	source := extractUnitSource(codesPath, unit)
	if strings.TrimSpace(source) == "" {
		return plugins.RadarResult{Decision: plugins.DecisionPass}
	}

	// 1. 去除 docstring 和注释
	clean := pyDocstringPattern.ReplaceAllString(source, "")
	clean = pySingleCommentPattern.ReplaceAllString(clean, "")

	// 2. 基于首行相对缩进提取纯函数体，避免参数类型注解冒号截断失真
	body := extractPythonIndentBody(clean)
	trimmed := strings.TrimSpace(body)

	// 3. Tier 0 极速确诊：仅 pass / ... / 空函数体 / 仅 print 日志
	if trimmed == "" || trimmed == "pass" || trimmed == "..." {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_EMPTY_STUB", "Python 测试用例为空桩 (pass/...)", "测试函数体仅为 pass、省略号或空体，缺乏有效执行与断言逻辑。", source),
			Reason:   "pure empty or pass stub",
		}
	}

	strippedPrint := pyPrintPattern.ReplaceAllString(body, "")
	strippedTrimmed := strings.TrimSpace(strippedPrint)
	if strippedTrimmed == "" || strippedTrimmed == "pass" || strippedTrimmed == "..." {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_LOG_ONLY_STUB", "Python 测试用例仅包含 print 日志", "测试用例内无实质断言校验，仅包含单纯打印或日志输出。", source),
			Reason:   "logging-only micro stub",
		}
	}

	// 4. Anti-Cheating Guard 恒真检测与 Mock 笔误排查
	var hints []plugins.StructuralFactHint
	if pyLiteralTautologyPattern.MatchString(body) || hasDeepSelfEqual(body, deepPyEqPattern) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到字面量恒真或自等断言（如 assert True 或 assert x == x），断言恒真。",
		})
	}
	if pyMockTypoPattern.MatchString(body) && !pyMockAssertPattern.MatchString(body) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "MOCK_ASSERTION_TYPO",
			Description: "检测到疑似 Mock 断言方法拼写笔误（如 asser_called 代替 assert_called），可能导致断言静默失效。",
		})
	}

	// 5. 控制流异常与静默异常捕获嗅探
	if hasPyUnguardedReturn(body) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "UNGUARDED_EARLY_RETURN",
			Description: "用例内部存在条件提前 return 分支，可能导致后续断言被跳过形成假通过隐患。",
		})
	}
	if hasPySwallowedExcept(body) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "SWALLOWED_EXCEPTION",
			Description: "检测到 except 块内仅 pass/打日志而未唤起用例失败，存在异常被静默吞噬风险。",
		})
	}

	// 6. 断言强度与密度分级
	strongMatches := pyStrongAssertPattern.FindAllString(body, -1)
	weakMatches := pyWeakAssertPattern.FindAllString(body, -1)
	hasStdAssert := pyStdAssertPattern.MatchString(body)
	hasMockAssert := pyMockAssertPattern.MatchString(body)
	hasHelper := pyHelperPattern.MatchString(body)
	effLines := countEffectiveLines(body)

	if len(strongMatches) == 0 && len(weakMatches) == 1 && effLines >= 4 {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "ISOLATED_WEAK_ASSERTION",
			Description: "用例仅包含单个非空/存在性弱断言，缺乏对核心业务属性与期望值的强比对验证。",
		})
	}

	// 7. Fast Pass 放行黄金准则：
	// - 包含强断言（或极简短用例带弱断言，或规范 mock 期望校验）
	// - 且无任何恒真作弊、无提前 return、无空 except、无孤立弱断言
	canFastPass := (len(strongMatches) > 0 || (effLines < 4 && len(weakMatches) > 0) || hasMockAssert) && len(hints) == 0
	if canFastPass && (hasStdAssert || hasMockAssert) {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Finding:  buildFastPassFinding(unit, "前置门禁校验通过：包含规范强断言或Mock校验且无控制流异常或弱断言作弊模式。"),
			Reason:   "contains standard strong assertions/mock checks and passed quality guards",
		}
	}

	// 8. Need Verify 争议区
	if hasHelper {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "POTENTIAL_HELPER_VERIFICATION",
			Description: "未检测到显式 assert/mock 语句，但调用了验证类辅助函数。",
		})
	} else if !hasStdAssert && !hasMockAssert {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "NO_EXPLICIT_ASSERTION",
			Description: "未检测到显式断言语句，需复核是否存在异常期望、上下文管理器验证或属于无效覆盖桩。",
		})
	}

	return plugins.RadarResult{
		Decision:  plugins.DecisionNeedVerify,
		FactHints: hints,
		Reason:    "requires semantic verification",
	}
}
