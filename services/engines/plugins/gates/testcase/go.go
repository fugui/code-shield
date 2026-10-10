package testcase

import (
	"path/filepath"
	"regexp"
	"strings"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

var (
	goStdAssertPattern        = regexp.MustCompile(`\b(t\.(Error|Errorf|Fatal|Fatalf|Fail|FailNow)|assert\.[A-Za-z0-9_]+|require\.[A-Za-z0-9_]+)`)
	goTableDriverPattern      = regexp.MustCompile(`(?s)for\s+.*,\s*(?:tt|tc|test)\s*:=\s*range\s+.*t\.Run\(`)
	goTautologyLiteralPattern = regexp.MustCompile(`\b(assert\.True\s*\(\s*t\s*,\s*true\s*\)|assert\.False\s*\(\s*t\s*,\s*false\s*\))`)
	goAssertEqualPattern      = regexp.MustCompile(`\bassert\.Equal\s*\(\s*t\s*,\s*([a-zA-Z0-9_]+)\s*,\s*([a-zA-Z0-9_]+)\s*\)`)
	goLoggingPattern          = regexp.MustCompile(`(?m)^\s*(log\.(Print|Println|Printf)|fmt\.(Print|Println|Printf))[^)]*\)?\s*$`)
	goHelperPattern           = regexp.MustCompile(`\b(check[A-Za-z0-9_]*|verify[A-Za-z0-9_]*|assert[A-Za-z0-9_]*)\s*\(\s*t\b`)
	goSingleCommentPattern    = regexp.MustCompile(`//.*`)
	goMultiCommentPattern     = regexp.MustCompile(`/\*[\s\S]*?\*/`)
)

func hasGoSelfEqTautology(code string) bool {
	matches := goAssertEqualPattern.FindAllStringSubmatch(code, -1)
	for _, m := range matches {
		if len(m) >= 3 && m[1] == m[2] {
			return true
		}
	}
	return false
}

// GoLinterRadar Go 特化轻量雷达探针
type GoLinterRadar struct{}

func (r *GoLinterRadar) CanHandle(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".go"
}

func (r *GoLinterRadar) InspectUnit(codesPath string, unit coverage.PlanUnit) plugins.RadarResult {
	source := extractUnitSource(codesPath, unit)
	if strings.TrimSpace(source) == "" {
		return plugins.RadarResult{Decision: plugins.DecisionPass}
	}

	body := extractBraceBody(source)
	clean := goMultiCommentPattern.ReplaceAllString(body, "")
	clean = goSingleCommentPattern.ReplaceAllString(clean, "")
	trimmed := strings.TrimSpace(clean)

	// 1. Tier 0 极速确诊：纯空函数体或仅包含日志打印
	if trimmed == "" {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_EMPTY_STUB", "Go 测试用例函数体为空桩", "测试函数体无任何可执行代码与断言逻辑，属于完全空桩用例。", source),
			Reason:   "pure empty function body",
		}
	}

	strippedLog := goLoggingPattern.ReplaceAllString(clean, "")
	if strings.TrimSpace(strippedLog) == "" {
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  buildTier0Finding(unit, "UT_LOG_ONLY_STUB", "Go 测试用例仅包含 fmt/log 打印", "测试函数内仅包含日志打印，未调用 t.Error/t.Fatal 或 testify 断言，测试有效性缺失。", source),
			Reason:   "logging-only micro stub",
		}
	}

	// 2. Anti-Cheating Guard 恒真检测
	var hints []plugins.StructuralFactHint
	if goTautologyLiteralPattern.MatchString(clean) || hasGoSelfEqTautology(clean) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到 Go 测试用例包含 assert.True(t, true) 或 assert.Equal 自等比较恒真断言。",
		})
	}

	hasStdAssert := goStdAssertPattern.MatchString(clean)
	hasTableDriver := goTableDriverPattern.MatchString(clean)
	hasHelper := goHelperPattern.MatchString(clean)

	// 3. Fast Pass 放行：
	// - 包含标准断言且无恒真作弊
	// - 或者为标准表格驱动测试 (for ... range tests { t.Run(...) }) 且内部有断言
	if (hasStdAssert || (hasTableDriver && hasStdAssert)) && len(hints) == 0 {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Reason:   "contains standard assertions/table-driven tests and no cheating patterns",
		}
	}

	// 4. Need Verify 争议区
	if hasHelper {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "HELPER_ASSERTION_CALL",
			Description: "调用了封装的 Helper 函数（传 t 入参），需复核 Helper 函数内部是否包含断言。",
		})
	} else if !hasStdAssert {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "NO_EXPLICIT_ASSERTION",
			Description: "未检测到 t.Error/t.Fatal 或 assert 语句，需复核是否存在间接校验或属于无效覆盖桩。",
		})
	}

	return plugins.RadarResult{
		Decision:  plugins.DecisionNeedVerify,
		FactHints: hints,
		Reason:    "requires semantic verification",
	}
}
