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
	goStrongAssertPattern     = regexp.MustCompile(`\b(assert\.(Equal|Equalf|Same|NotEqual|Contains|ElementsMatch|InDelta|InEpsilon|JSONEq|YAMLEq|Len)|require\.(Equal|Equalf|Contains|ElementsMatch|Len)|t\.(Errorf|Fatalf|Fail|FailNow))\b`)
	goWeakAssertPattern       = regexp.MustCompile(`\b(assert\.(NotNil|Nil|NoError|True|False)|require\.(NotNil|Nil|NoError))\b`)
	goTableDriverPattern      = regexp.MustCompile(`(?s)for\s+.*,\s*(?:tt|tc|test)\s*:=\s*range\s+.*t\.Run\(`)
	goTautologyLiteralPattern = regexp.MustCompile(`\b(assert\.True\s*\(\s*t\s*,\s*true\s*\)|assert\.False\s*\(\s*t\s*,\s*false\s*\))`)
	goLoggingPattern          = regexp.MustCompile(`(?m)^\s*(log\.(Print|Println|Printf)|fmt\.(Print|Println|Printf))[^)]*\)?\s*$`)
	goHelperPattern           = regexp.MustCompile(`\b(check[A-Za-z0-9_]*|verify[A-Za-z0-9_]*|assert[A-Za-z0-9_]*)\s*\(\s*t\b`)
	goSingleCommentPattern    = regexp.MustCompile(`//.*`)
	goMultiCommentPattern     = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	goUnguardedReturnPattern  = regexp.MustCompile(`(?s)if\s+[^{]+\{[^}]*?\breturn(?:\s+[^\n}]+)?\s*\}`)
	goSwallowedErrPattern     = regexp.MustCompile(`(?s)if\s+(?:[a-zA-Z0-9_]+\s*:=\s*[^;]+;\s*)?err\s*!=\s*nil\s*\{\s*(?:log\.[^}]*|fmt\.[^}]*|return[^}]*)?\s*\}`)
)

func hasGoUnguardedReturn(code string) bool {
	matches := goUnguardedReturnPattern.FindAllString(code, -1)
	for _, m := range matches {
		if !strings.Contains(m, "t.Fatal") && !strings.Contains(m, "t.Error") && !strings.Contains(m, "t.Fail") {
			return true
		}
	}
	return false
}

func hasGoSwallowedError(code string) bool {
	matches := goSwallowedErrPattern.FindAllString(code, -1)
	for _, m := range matches {
		if !strings.Contains(m, "t.Fatal") && !strings.Contains(m, "t.Error") && !strings.Contains(m, "t.Fail") {
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

	body := extractBraceBodyBalanced(source)
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
	if goTautologyLiteralPattern.MatchString(clean) || hasDeepSelfEqual(clean, deepGoEqPattern) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "TAUTOLOGY_LITERAL",
			Description: "检测到 Go 测试用例包含 assert.True(t, true) 或 assert.Equal 自等比较恒真断言。",
		})
	}

	// 3. 控制流异常与静默异常捕获嗅探
	if hasGoUnguardedReturn(clean) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "UNGUARDED_EARLY_RETURN",
			Description: "用例内部存在条件提前 return 分支，可能导致后续断言被跳过形成假通过隐患。",
		})
	}
	if hasGoSwallowedError(clean) {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "SWALLOWED_EXCEPTION",
			Description: "检测到 if err != nil 错误分支内未唤起测试失败 (t.Error/t.Fatal)，存在错误被静默吞噬风险。",
		})
	}

	// 4. 断言强度与密度分级
	strongMatches := goStrongAssertPattern.FindAllString(clean, -1)
	weakMatches := goWeakAssertPattern.FindAllString(clean, -1)
	hasStdAssert := goStdAssertPattern.MatchString(clean)
	hasTableDriver := goTableDriverPattern.MatchString(clean)
	hasHelper := goHelperPattern.MatchString(clean)
	effLines := countEffectiveLines(clean)

	if len(strongMatches) == 0 && len(weakMatches) == 1 && effLines >= 4 {
		hints = append(hints, plugins.StructuralFactHint{
			Kind:        "ISOLATED_WEAK_ASSERTION",
			Description: "用例仅包含单个非空/无错误弱断言，缺乏对核心业务返回值具体属性的实质校验。",
		})
	}

	// 5. Fast Pass 放行黄金准则：
	// - 包含强断言（或极简短用例带弱断言，或表格驱动测试）
	// - 且无任何恒真作弊、无提前 return、无静默吞错、无孤立弱断言
	canFastPass := (len(strongMatches) > 0 || (effLines < 4 && len(weakMatches) > 0) || hasTableDriver) && len(hints) == 0
	if canFastPass && hasStdAssert {
		return plugins.RadarResult{
			Decision: plugins.DecisionFastPass,
			Finding:  buildFastPassFinding(unit, "前置门禁校验通过：包含规范强断言且无控制流异常或弱断言作弊模式。"),
			Reason:   "contains standard strong assertions/table-driven tests and passed quality guards",
		}
	}

	// 6. Need Verify 争议区
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
