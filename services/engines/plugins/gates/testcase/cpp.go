package testcase

import (
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

var (
	cppStdAssertPattern         = regexp.MustCompile(`\b(EXPECT_|ASSERT_|CPPUNIT_ASSERT|BOOST_CHECK|CHECK\(|REQUIRE\()`)
	cppStrongAssertPattern      = regexp.MustCompile(`\b(EXPECT_EQ|ASSERT_EQ|EXPECT_NE|ASSERT_NE|EXPECT_STREQ|ASSERT_STREQ|EXPECT_STRNE|ASSERT_STRNE|EXPECT_LT|EXPECT_LE|EXPECT_GT|EXPECT_GE|EXPECT_THAT|ASSERT_THAT|CHECK_EQ|REQUIRE_EQ|BOOST_CHECK_EQUAL|EXPECT_CALL)\b`)
	cppWeakAssertPattern        = regexp.MustCompile(`\b(EXPECT_TRUE|ASSERT_TRUE|EXPECT_FALSE|ASSERT_FALSE|CHECK|REQUIRE|BOOST_CHECK)\b`)
	cppExpectCallOrThrowPattern = regexp.MustCompile(`\b(EXPECT_CALL|ON_CALL|EXPECT_THROW|EXPECT_NO_THROW|ASSERT_THROW|ASSERT_NO_THROW)\b`)
	cppTautologyLiteralPattern  = regexp.MustCompile(`\b(EXPECT_TRUE\s*\(\s*(true|1)\s*\)|EXPECT_FALSE\s*\(\s*(false|0)\s*\)|ASSERT_TRUE\s*\(\s*(true|1)\s*\)|ASSERT_FALSE\s*\(\s*(false|0)\s*\))`)
	cppLoggingPattern           = regexp.MustCompile(`(?m)^\s*(LOG\s*\(|printf\s*\(|std::cout\b|cout\s*<<)[^;]*;?\s*$`)
	cppHelperCallPattern        = regexp.MustCompile(`\b(verify[A-Za-z0-9_]*|check[A-Za-z0-9_]*|assert[A-Za-z0-9_]*|validate[A-Za-z0-9_]*)\s*\(`)
	cppSingleCommentPattern     = regexp.MustCompile(`//.*`)
	cppMultiCommentPattern      = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	cppUnguardedReturnPattern   = regexp.MustCompile(`(?s)if\s*\([^{]+\)\s*\{[^}]*?\breturn(?:\s+[^;]+)?;?\s*\}`)
	cppSwallowedCatchPattern    = regexp.MustCompile(`(?s)catch\s*\([^)]*\)\s*\{\s*(?:LOG[^;}]*;|printf[^;}]*;|std::cout[^;}]*;)?\s*\}`)
)

func hasCppUnguardedReturn(code string) bool {
	matches := cppUnguardedReturnPattern.FindAllString(code, -1)
	for _, m := range matches {
		if !strings.Contains(m, "FAIL") && !strings.Contains(m, "ADD_FAILURE") {
			return true
		}
	}
	return false
}

func hasCppSwallowedCatch(code string) bool {
	matches := cppSwallowedCatchPattern.FindAllString(code, -1)
	for _, m := range matches {
		if !strings.Contains(m, "FAIL") && !strings.Contains(m, "ADD_FAILURE") && !strings.Contains(m, "throw") {
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

	// 1. 纯 Go 词法栈抽取函数体并去除注释
	body := extractBraceBodyBalanced(source)
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
	if cppTautologyLiteralPattern.MatchString(cleanBody) || hasDeepSelfEqual(cleanBody, deepCppEqPattern) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到字面量恒真或自等比较断言（如 EXPECT_TRUE(true) 或 EXPECT_EQ(x, x)），断言结果恒为真。",
		})
	}

	// 4. 控制流异常与静默异常捕获嗅探
	if hasCppUnguardedReturn(cleanBody) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "UNGUARDED_EARLY_RETURN",
			Description: "用例内部存在条件提前 return 分支，可能导致后续断言被跳过形成假通过隐患。",
		})
	}
	if hasCppSwallowedCatch(cleanBody) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "SWALLOWED_EXCEPTION",
			Description: "检测到 catch (...) 块内未调用 FAIL()/ADD_FAILURE() 或重新抛出，存在异常被吞噬隐患。",
		})
	}

	// 5. 断言强度与密度分级
	strongMatches := cppStrongAssertPattern.FindAllString(cleanBody, -1)
	weakMatches := cppWeakAssertPattern.FindAllString(cleanBody, -1)
	hasStdAssert := cppStdAssertPattern.MatchString(cleanBody)
	hasExpectCallOrThrow := cppExpectCallOrThrowPattern.MatchString(cleanBody)
	hasHelperCall := cppHelperCallPattern.MatchString(cleanBody)
	effLines := countEffectiveLines(cleanBody)

	if len(strongMatches) == 0 && len(weakMatches) == 1 && effLines >= 4 {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "ISOLATED_WEAK_ASSERTION",
			Description: "用例仅包含单个布尔弱断言，缺乏对核心业务属性与期望值的强比对验证。",
		})
	}

	// 6. Fast Pass 放行黄金准则：
	// - 包含强断言（或极简短用例包含弱断言）
	// - 且无任何恒真作弊、无提前 return、无空 catch、无孤立弱断言
	canFastPass := (len(strongMatches) > 0 || (effLines < 4 && len(weakMatches) > 0)) && len(hints) == 0
	if canFastPass && hasStdAssert {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Finding:  buildFastPassFinding(unit, "前置门禁校验通过：包含规范强断言且无控制流异常或弱断言作弊模式。"),
			Reason:   "contains standard strong assertions and passed quality guards",
		}
	}

	// 7. Need Verify 争议区
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

func cleanComments(code string) string {
	cleaned := cppMultiCommentPattern.ReplaceAllString(code, "")
	cleaned = cppSingleCommentPattern.ReplaceAllString(cleaned, "")
	return cleaned
}
