package testcase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/datatypes"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

// LinterRadar 多语言测试用例轻量门禁探针接口
type LinterRadar interface {
	CanHandle(filePath string) bool
	InspectUnit(codesPath string, unit coverage.PlanUnit) plugins.RadarResult
}

// RadarRegistry 多语言探针注册中心
type RadarRegistry struct {
	radars []LinterRadar
}

func (r *RadarRegistry) Register(radar LinterRadar) {
	r.radars = append(r.radars, radar)
}

func (r *RadarRegistry) Inspect(codesPath string, unit coverage.PlanUnit) plugins.RadarResult {
	for _, radar := range r.radars {
		if radar.CanHandle(unit.Path) {
			return radar.InspectUnit(codesPath, unit)
		}
	}
	// 无匹配语言探针时，直通进入后续阶段
	return plugins.RadarResult{
		Decision: plugins.DecisionPass,
		Reason:   fmt.Sprintf("no specialized linter radar for file %s", unit.Path),
	}
}

// TestCaseRadarGate 实现 plugins.PreflightGatePlugin 接口
type TestCaseRadarGate struct {
	registry *RadarRegistry
}

func NewTestCaseRadarGate() *TestCaseRadarGate {
	gate := &TestCaseRadarGate{registry: &RadarRegistry{}}
	gate.registry.Register(&CppLinterRadar{})
	gate.registry.Register(&PythonLinterRadar{})
	gate.registry.Register(&GoLinterRadar{})
	return gate
}

func (g *TestCaseRadarGate) ID() string {
	return "test_case_radar"
}

func (g *TestCaseRadarGate) Inspect(codesPath string, unit coverage.PlanUnit, options map[string]any) plugins.RadarResult {
	return g.registry.Inspect(codesPath, unit)
}

func init() {
	_ = plugins.RegisterPreflightGate(NewTestCaseRadarGate())
}

// extractUnitSource 从本地代码仓中根据 PlanUnit 行号提取被测代码
func extractUnitSource(codesPath string, unit coverage.PlanUnit) string {
	absPath := filepath.Join(codesPath, unit.Path)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	start := unit.StartLine - 1
	if start < 0 {
		start = 0
	}
	end := unit.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start >= len(lines) || start >= end {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

// buildTier0Finding 构造 Tier 0 静态截断缺陷 Finding
func buildTier0Finding(unit coverage.PlanUnit, ruleID, title, detail, snippet string) *models.AnalysisFinding {
	lineStr := fmt.Sprintf("%d", unit.StartLine)
	if unit.EndLine > unit.StartLine {
		lineStr = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}

	fingerprintRaw := fmt.Sprintf("%s:%s:%s:%s", unit.Path, unit.ID, ruleID, unit.DisplayName)
	sum := sha256.Sum256([]byte(fingerprintRaw))
	observationUID := hex.EncodeToString(sum[:])

	return &models.AnalysisFinding{
		PrimaryUnitID:       unit.ID,
		Severity:            "致命",
		Category:            "断言有效性-空测试",
		CategoryCode:        "UTE_EMPTY_TEST",
		CategorySource:      "preflight_gate",
		CategoryStatus:      "active",
		FilePath:            unit.Path,
		LineNumber:          lineStr,
		CodeSnippet:         snippet,
		Title:               title,
		Detail:              detail,
		Suggestion:          "请补充对被测对象核心返回值、状态变更或异常预期的实质性断言验证，严禁提交无断言或微空桩测试。",
		CalibrationRule:     ruleID,
		ObservationGroupUID: observationUID,
		AnchorConfidence:    "HIGH",
		AssessmentStatus:    "DEFECT",
		AssessmentOutcome:   "DEFECT",
	}
}

// buildFastPassFinding 构造测试用例快速放行（合格）Finding
func buildFastPassFinding(unit coverage.PlanUnit, reason string) *models.AnalysisFinding {
	displayName := unit.DisplayName
	if displayName == "" {
		displayName = unit.Path
	}
	lineStr := fmt.Sprintf("%d", unit.StartLine)
	if unit.EndLine > unit.StartLine {
		lineStr = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}
	detail := reason
	if detail == "" {
		detail = "前置门禁校验通过：测试用例包含规范断言且无作弊或空测试模式。"
	}

	rawArtifact, _ := json.Marshal(map[string]any{
		"unit_ref": unit.DisplayName,
		"outcome":  "pass",
		"summary":  detail,
	})

	return &models.AnalysisFinding{
		PrimaryUnitID:      unit.ID,
		AssessmentStatus:   "pass",
		AssessmentOutcome:  "pass",
		AssessmentArtifact: datatypes.JSON(rawArtifact),
		FilePath:           unit.Path,
		LineNumber:         lineStr,
		Category:           "无问题",
		CategorySource:     "preflight_gate",
		CategoryStatus:     "LEGACY",
		Severity:           "合格",
		Title:              fmt.Sprintf("测试用例合格：%s", displayName),
		Detail:             detail,
	}
}

// extractBraceBodyBalanced 使用纯 Go 词法状态机平衡匹配顶层大括号，避免嵌套闭包与字面量误导
func extractBraceBodyBalanced(source string) string {
	firstBrace := strings.Index(source, "{")
	if firstBrace < 0 {
		return source
	}

	depth := 0
	inString := false
	inChar := false
	inLineComment := false
	inBlockComment := false
	var strQuote byte

	startIdx := -1
	endIdx := -1

	for i := 0; i < len(source); i++ {
		ch := source[i]
		next := byte(0)
		if i+1 < len(source) {
			next = source[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if ch == '\\' {
				i++ // 跳过转义字符
			} else if ch == strQuote {
				inString = false
			}
			continue
		}
		if inChar {
			if ch == '\\' {
				i++
			} else if ch == '\'' {
				inChar = false
			}
			continue
		}

		// 检查注释开始
		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}

		// 检查字符串/字符开始
		if ch == '"' || ch == '`' {
			inString = true
			strQuote = ch
			continue
		}
		if ch == '\'' {
			inChar = true
			continue
		}

		// 括号计数
		if ch == '{' {
			if depth == 0 {
				startIdx = i + 1
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				endIdx = i
				break
			}
		}
	}

	if startIdx >= 0 && endIdx >= startIdx {
		return source[startIdx:endIdx]
	}
	// 回退到朴素截取
	lastBrace := strings.LastIndex(source, "}")
	if firstBrace >= 0 && lastBrace > firstBrace {
		return source[firstBrace+1 : lastBrace]
	}
	return source
}

// extractPythonIndentBody 基于缩进块提取 Python 函数体，避免参数类型注解冒号的误截断
func extractPythonIndentBody(source string) string {
	lines := strings.Split(source, "\n")
	if len(lines) == 0 {
		return source
	}

	defLineIdx := -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "def ") {
			defLineIdx = i
			break
		}
	}
	if defLineIdx < 0 {
		colonIdx := strings.Index(source, ":")
		if colonIdx >= 0 {
			return source[colonIdx+1:]
		}
		return source
	}

	// 找到包含函数定义结束冒号的那一行
	colonLineIdx := defLineIdx
	for i := defLineIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasSuffix(trimmed, ":") {
			colonLineIdx = i
			break
		}
	}

	// 确定第一条函数体内语句的缩进长度
	baseIndent := -1
	var bodyLines []string
	for i := colonLineIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		// 计算当前行前导空格数
		curIndent := len(line) - len(strings.TrimLeft(line, " \t"))
		if baseIndent < 0 {
			baseIndent = curIndent
		}
		if curIndent < baseIndent {
			// 缩进回退，说明函数体已结束
			break
		}
		bodyLines = append(bodyLines, line)
	}

	if len(bodyLines) == 0 {
		// 回退到原朴素截取
		colonIdx := strings.Index(source, ":")
		if colonIdx >= 0 {
			return source[colonIdx+1:]
		}
		return source
	}
	return strings.Join(bodyLines, "\n")
}

// countEffectiveLines 统计有效代码行数（剔除空行和纯注释行）
func countEffectiveLines(code string) int {
	lines := strings.Split(code, "\n")
	count := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		count++
	}
	return count
}

var (
	deepCppEqPattern = regexp.MustCompile(`\b(?:EXPECT_EQ|ASSERT_EQ)\s*\(\s*([^,]+?)\s*,\s*([^,)]+?)\s*\)`)
	deepGoEqPattern  = regexp.MustCompile(`\bassert\.Equal\s*\(\s*[^,]+,\s*([^,]+?)\s*,\s*([^,)]+?)\s*\)`)
	deepPyEqPattern  = regexp.MustCompile(`(?m)^\s*(?:assert\s+|self\.assertEqual\s*\(\s*)(.+?)\s*==\s*([^)]+?)\s*(?:\)|$)`)
)

// hasDeepSelfEqual 识别带点号或复杂表达式的自等比较恒真（如 resp.data.id == resp.data.id）
func hasDeepSelfEqual(code string, pattern *regexp.Regexp) bool {
	matches := pattern.FindAllStringSubmatch(code, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			left := strings.TrimSpace(m[1])
			right := strings.TrimSpace(m[2])
			if left != "" && left == right {
				return true
			}
		}
	}
	return false
}
