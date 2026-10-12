package testcase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
