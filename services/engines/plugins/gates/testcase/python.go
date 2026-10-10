package testcase

import (
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

var (
	pyStdAssertPattern       = regexp.MustCompile(`(?m)^\s*assert\s+|self\.assert|pytest\.raises|mocker\.`)
	pyMockAssertPattern      = regexp.MustCompile(`\b(assert_called|assert_called_once|assert_called_with|assert_called_once_with|assert_any_call|assert_has_calls|assert_not_called)\b`)
	pyMockTypoPattern        = regexp.MustCompile(`\b(asser_called|assert_caled|assert_called_wth|assert_calld)\b`)
	pyLiteralTautologyPattern = regexp.MustCompile(`\bassert\s+(True|1|not\s+False|\([Tt]rue\))\b`)
	pyAssertEqPattern        = regexp.MustCompile(`\bassert\s+([a-zA-Z0-9_]+)\s*==\s*([a-zA-Z0-9_]+)\b`)
	pyHelperPattern          = regexp.MustCompile(`\b(verify_[A-Za-z0-9_]*|check_[A-Za-z0-9_]*|assert_[A-Za-z0-9_]*|validate_[A-Za-z0-9_]*)\s*\(`)
	pyPrintPattern           = regexp.MustCompile(`(?m)^\s*(print\s*\(|logging\.|logger\.)[^)]*\)?\s*$`)
	pyDocstringPattern       = regexp.MustCompile(`(?s)"""[\s\S]*?"""|'''[\s\S]*?'''`)
	pySingleCommentPattern   = regexp.MustCompile(`(?m)#.*$`)
)

func hasPySelfEqTautology(code string) bool {
	matches := pyAssertEqPattern.FindAllStringSubmatch(code, -1)
	for _, m := range matches {
		if len(m) >= 3 && m[1] == m[2] {
			return true
		}
	}
	return false
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

	// 提取函数体（去除 def test_xxx(...): 头部）
	body := clean
	colonIdx := strings.Index(clean, ":")
	if colonIdx >= 0 {
		body = clean[colonIdx+1:]
	}
	trimmed := strings.TrimSpace(body)

	// 2. Tier 0 极速确诊：仅 pass / ... / 空函数体 / 仅 print 日志
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

	// 3. Anti-Cheating Guard 恒真检测与 Mock 笔误排查
	var hints []plugins.StructuralFactHint
	if pyLiteralTautologyPattern.MatchString(body) || hasPySelfEqTautology(body) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到字面量恒真断言（如 assert True 或 assert x == x），断言恒真。",
		})
	}
	if pyMockTypoPattern.MatchString(body) && !pyMockAssertPattern.MatchString(body) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "MOCK_ASSERTION_TYPO",
			Description: "检测到疑似 Mock 断言方法拼写笔误（如 asser_called 代替 assert_called），可能导致断言静默失效。",
		})
	}

	hasStdAssert := pyStdAssertPattern.MatchString(body)
	hasMockAssert := pyMockAssertPattern.MatchString(body)
	hasHelper := pyHelperPattern.MatchString(body)

	// 4. Fast Pass 放行：包含标准 assert / pytest / mock 且无作弊嫌疑
	if (hasStdAssert || hasMockAssert) && len(hints) == 0 {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Reason:   "contains standard assertions and no cheating patterns",
		}
	}

	// 5. Need Verify 争议区
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
