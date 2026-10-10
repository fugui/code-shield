package testcase

import (
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

var (
	cppStdAssertPattern          = regexp.MustCompile(`\b(EXPECT_|ASSERT_|CPPUNIT_ASSERT|BOOST_CHECK|CHECK\(|REQUIRE\()`)
	cppExpectCallOrThrowPattern  = regexp.MustCompile(`\b(EXPECT_CALL|ON_CALL|EXPECT_THROW|EXPECT_NO_THROW|ASSERT_THROW|ASSERT_NO_THROW)\b`)
	cppTautologyLiteralPattern   = regexp.MustCompile(`\b(EXPECT_TRUE\s*\(\s*(true|1)\s*\)|EXPECT_FALSE\s*\(\s*(false|0)\s*\)|ASSERT_TRUE\s*\(\s*(true|1)\s*\)|ASSERT_FALSE\s*\(\s*(false|0)\s*\))`)
	cppEqPattern                 = regexp.MustCompile(`\b(?:EXPECT_EQ|ASSERT_EQ)\s*\(\s*([a-zA-Z0-9_]+)\s*,\s*([a-zA-Z0-9_]+)\s*\)`)
	cppLoggingPattern            = regexp.MustCompile(`(?m)^\s*(LOG\s*\(|printf\s*\(|std::cout\b|cout\s*<<)[^;]*;?\s*$`)
	cppHelperCallPattern         = regexp.MustCompile(`\b(verify[A-Za-z0-9_]*|check[A-Za-z0-9_]*|assert[A-Za-z0-9_]*|validate[A-Za-z0-9_]*)\s*\(`)
	cppSingleCommentPattern      = regexp.MustCompile(`//.*`)
	cppMultiCommentPattern       = regexp.MustCompile(`/\*[\s\S]*?\*/`)
)

func hasCppSelfEqTautology(code string) bool {
	matches := cppEqPattern.FindAllStringSubmatch(code, -1)
	for _, m := range matches {
		if len(m) >= 3 && m[1] == m[2] {
			return true
		}
	}
	return false
}

// CppLinterRadar C++ 特化轻量雷达探针
type CppLinterRadar struct{}

func (r *CppLinterRadar) CanHandle(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".cpp" || ext == ".cc" || ext == ".cxx" || ext == ".c++" || ext == ".c"
}

func (r *CppLinterRadar) InspectUnit(codesPath string, unit coverage.PlanUnit) plugins.RadarResult {
	source := extractUnitSource(codesPath, unit)
	if strings.TrimSpace(source) == "" {
		return plugins.RadarResult{Decision: plugins.DecisionPass}
	}

	// 1. 抽取函数体并去除注释
	body := extractBraceBody(source)
	cleanBody := cleanComments(body)
	trimmed := strings.TrimSpace(cleanBody)

	// 2. Tier 0 极速确诊：纯空体 {} 或微空桩（仅声明/仅日志打印）
	if trimmed == "" {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_EMPTY_STUB", "C++ 测试用例函数体为空桩", "测试函数内无任何有效执行语句与断言验证，属于完全空桩用例。", source),
			Reason:   "pure empty function body",
		}
	}

	// 去除所有单纯日志行后是否为空
	strippedLogs := cppLoggingPattern.ReplaceAllString(cleanBody, "")
	if strings.TrimSpace(strippedLogs) == "" {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_LOG_ONLY_STUB", "C++ 测试用例仅包含日志打印", "测试函数内仅包含日志输出，缺乏任何断言校验，不具备实质性测试有效性。", source),
			Reason:   "logging-only micro stub",
		}
	}

	// 3. Anti-Cheating Guard 恒真检测
	var hints []plugins.StructuralFactHint
	if cppTautologyLiteralPattern.MatchString(cleanBody) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到字面量恒真断言（如 EXPECT_TRUE(true) 或 ASSERT_FALSE(0)），断言结果恒为真。",
		})
	}
	if hasCppSelfEqTautology(cleanBody) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_SELF_EQUAL",
			Description: "检测到自等比较恒真断言（如 EXPECT_EQ(x, x)），无法验证外部被测对象逻辑。",
		})
	}

	hasStdAssert := cppStdAssertPattern.MatchString(cleanBody)
	hasExpectCallOrThrow := cppExpectCallOrThrowPattern.MatchString(cleanBody)
	hasHelperCall := cppHelperCallPattern.MatchString(cleanBody)

	// 4. Fast Pass 放行：包含标准 GoogleTest / Catch2 断言且无恒真作弊嫌疑
	if hasStdAssert && len(hints) == 0 {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Reason:   "contains standard assertions and no cheating patterns",
		}
	}

	// 5. Need Verify 争议区：有异常/Mock契约、Helper校验、或作弊疑点
	if hasExpectCallOrThrow {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "EXCEPTION_OR_MOCK_CONTRACT",
			Description: "包含 EXPECT_THROW/EXPECT_NO_THROW 或 EXPECT_CALL 契约验证，需复核语义有效性。",
		})
	}
	if hasHelperCall {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "HELPER_ASSERTION_CALL",
			Description: "调用了封装的测试辅助函数（如 verify.../check...），需复核辅助函数内是否包含实质断言。",
		})
	}
	if !hasStdAssert && !hasExpectCallOrThrow && !hasHelperCall {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "NO_EXPLICIT_ASSERTION",
			Description: "未检测到显式断言语句，需复核是否存在隐式校验或属于无效覆盖桩。",
		})
	}

	return plugins.RadarResult{
		Decision:  plugins.DecisionNeedVerify,
		FactHints: hints,
		Reason:    "requires semantic verification",
	}
}

func extractBraceBody(source string) string {
	firstBrace := strings.Index(source, "{")
	lastBrace := strings.LastIndex(source, "}")
	if firstBrace >= 0 && lastBrace > firstBrace {
		return source[firstBrace+1 : lastBrace]
	}
	return source
}

func cleanComments(code string) string {
	cleaned := cppMultiCommentPattern.ReplaceAllString(code, "")
	cleaned = cppSingleCommentPattern.ReplaceAllString(cleaned, "")
	return cleaned
}
