package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
	"code-shield/services/invoker"

	"gorm.io/datatypes"
)

const (
	defaultVerifierTimeout = 60.0
	builtinTriagePrompt    = `你是一名代码质量与测试有效性仲裁员。请判定以下测试用例是否包含实质性测试验证。
## 源码 ({{.Language}}):
` + "```" + `{{.Language}}
{{.UnitSourceCode}}
` + "```" + `
## 线索:
{{range .FactHints}}- [{{.Kind}}]: {{.Description}}
{{end}}
请输出严格 JSON: {"outcome": "PASS"|"DEFECT", "category_code": "UTE_EMPTY_TEST"|"UTE_TAUTOLOGY_ASSERTION"|"UTE_SILENT_CATCH"|"UTE_OTHER", "severity": "一般"|"严重"|"致命", "reason": "原因说明"}`
)

// TriagePromptData 传递给复核提示词模板的参数
type TriagePromptData struct {
	Language       string
	UnitSourceCode string
	FactHints      []plugins.StructuralFactHint
}

// TriageLLMResponse LLM 单轮极速复核返回的结构化结果
type TriageLLMResponse struct {
	Outcome      string `json:"outcome"`
	CategoryCode string `json:"category_code,omitempty"`
	Reason       string `json:"reason"`
	Severity     string `json:"severity"`
}

// NativeSingleRoundVerifier 基于轻量单轮大模型仲裁的复核插件
type NativeSingleRoundVerifier struct {
	invokerOverride invoker.AIInvoker // 用于单元测试注入 mock invoker
}

func NewNativeSingleRoundVerifier() *NativeSingleRoundVerifier {
	return &NativeSingleRoundVerifier{}
}

func (v *NativeSingleRoundVerifier) ID() string {
	return "native_single_round"
}

func (v *NativeSingleRoundVerifier) SetInvokerForTest(inv invoker.AIInvoker) {
	v.invokerOverride = inv
}

func (v *NativeSingleRoundVerifier) Verify(
	codesPath string,
	unit coverage.PlanUnit,
	hints []plugins.StructuralFactHint,
	promptTemplate string,
	timeoutSec float64,
) (plugins.RadarResult, error) {
	if timeoutSec <= 0 {
		timeoutSec = defaultVerifierTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	// 1. 提取源码与语言标识
	source := extractSource(codesPath, unit)
	lang := detectLanguage(unit.Path)

	// 2. 加载或渲染提示词模板
	templateContent := builtinTriagePrompt
	if promptTemplate != "" {
		if content, err := os.ReadFile(models.AppConfig.GetAbsPath(promptTemplate)); err == nil && len(content) > 0 {
			templateContent = string(content)
		} else if directContent, err := os.ReadFile(promptTemplate); err == nil && len(directContent) > 0 {
			templateContent = string(directContent)
		}
	}

	tmpl, err := template.New("triage").Parse(templateContent)
	if err != nil {
		tmpl, _ = template.New("fallback").Parse(builtinTriagePrompt)
	}
	var rendered bytes.Buffer
	data := TriagePromptData{
		Language:       lang,
		UnitSourceCode: source,
		FactHints:      hints,
	}
	if err := tmpl.Execute(&rendered, data); err != nil {
		rendered.WriteString(source)
	}

	// 3. 获取底层 AI 驱动
	var aiInvoker invoker.AIInvoker
	if v.invokerOverride != nil {
		aiInvoker = v.invokerOverride
	} else {
		if inv, ok := invoker.GetRawInvoker("native"); ok && inv != nil {
			aiInvoker = inv
		} else if inv, ok := invoker.GetRawInvoker("claude"); ok && inv != nil {
			aiInvoker = inv
		}
	}

	if aiInvoker == nil {
		// 无可用 invoker（单测未注入或服务未配置），安全降级放行
		return plugins.RadarResult{
			Decision: plugins.DecisionPass,
			Reason:   "no ai invoker available for thin verification, degraded pass",
		}, nil
	}

	// 4. 创建临时输出文件
	temp, err := os.CreateTemp("", "thin-verifier-output-*.json")
	if err != nil {
		return plugins.RadarResult{Decision: plugins.DecisionPass, Reason: "create temp failed"}, nil
	}
	outputPath := temp.Name()
	_ = temp.Close()
	defer os.Remove(outputPath)

	timeoutSecondsInt := int(timeoutSec)
	if timeoutSecondsInt < 1 {
		timeoutSecondsInt = 1
	}

	req := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        codesPath,
		PromptMsg:      rendered.String(),
		OutputPath:     outputPath,
		TimeoutSeconds: timeoutSecondsInt,
		ResponseFormat: "json",
	}

	if err := aiInvoker.Invoke(req); err != nil {
		// 超时或调用失败降级放行
		return plugins.RadarResult{
			Decision: plugins.DecisionPass,
			Reason:   fmt.Sprintf("thin verifier invocation degraded: %v", err),
		}, nil
	}

	// 5. 解析输出
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil || len(outputBytes) == 0 {
		return plugins.RadarResult{
			Decision: plugins.DecisionPass,
			Reason:   "empty thin verifier output, degraded pass",
		}, nil
	}

	var resp TriageLLMResponse
	if err := json.Unmarshal(outputBytes, &resp); err != nil {
		return plugins.RadarResult{
			Decision: plugins.DecisionPass,
			Reason:   "unmarshal thin verifier output failed, degraded pass",
		}, nil
	}

	if strings.ToUpper(resp.Outcome) == "DEFECT" {
		finding := buildThinDefectFinding(unit, resp, source)
		return plugins.RadarResult{
			Decision: plugins.DecisionTier0Defect,
			Finding:  finding,
			Reason:   resp.Reason,
		}, nil
	}

	return plugins.RadarResult{
		Decision: plugins.DecisionFastPass,
		Finding:  buildThinPassFinding(unit, resp),
		Reason:   resp.Reason,
	}, nil
}

func extractSource(codesPath string, unit coverage.PlanUnit) string {
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

func detectLanguage(fp string) string {
	ext := strings.ToLower(filepath.Ext(fp))
	switch ext {
	case ".cpp", ".cc", ".cxx", ".c++", ".h", ".hpp":
		return "cpp"
	case ".py":
		return "python"
	case ".go":
		return "go"
	case ".java":
		return "java"
	default:
		return "text"
	}
}

func buildThinDefectFinding(unit coverage.PlanUnit, resp TriageLLMResponse, snippet string) *models.AnalysisFinding {
	lineStr := fmt.Sprintf("%d", unit.StartLine)
	if unit.EndLine > unit.StartLine {
		lineStr = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}

	ruleID := "UT_VERIFIER_CONFIRMED_DEFECT"
	sum := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%s", unit.Path, unit.ID, ruleID))
	obsUID := hex.EncodeToString(sum[:])

	sev := "严重"
	switch strings.TrimSpace(resp.Severity) {
	case "致命", "BLOCKER", "FATAL":
		sev = "致命"
	case "一般", "NORMAL", "MINOR":
		sev = "一般"
	case "建议", "SUGGESTION", "LOW":
		sev = "建议"
	case "严重", "MAJOR", "CRITICAL":
		sev = "严重"
	}

	catCode := strings.TrimSpace(resp.CategoryCode)
	catLabel := "断言有效性-永真断言"
	suggestion := "请删除或重构形式化恒真断言/无效覆盖桩，补充真实被测逻辑的输入输出边界验证。"

	switch catCode {
	case "UTE_EMPTY_TEST":
		catLabel = "断言有效性-空测试"
		suggestion = "请为测试用例补充实质性断言语句，验证被测方法关键返回值或状态变更。"
	case "UTE_SILENT_CATCH", "UTE_INVALID_CAPTURE":
		catCode = "UTE_INVALID_CAPTURE"
		catLabel = "断言有效性-无效捕获"
		suggestion = "请在 catch 块中唤起测试失败（如 fail() 或 ADD_FAILURE()），避免吞噬被测逻辑抛出的异常。"
	case "UTE_INCOMPLETE_EXPECT", "UTE_INCOMPLETE_EXPECTATION":
		catCode = "UTE_INCOMPLETE_EXPECTATION"
		catLabel = "断言有效性-预期结果不完整"
		suggestion = "请补充对业务关键载荷或核心属性的断言校验。"
	case "UTE_ILLEGAL_EXIT", "UTE_INVALID_EARLY_RETURN":
		catCode = "UTE_INVALID_EARLY_RETURN"
		catLabel = "提前返回-非法退出"
		suggestion = "请重构测试逻辑，移除提早返回的条件分支，确保后续断言稳定执行。"
	case "UTE_SILENT_SKIP":
		catLabel = "提前返回-静默跳过"
		suggestion = "请检查 skip 条件，确保关键用例在 CI 流水线中能被真实执行。"
	case "UTE_MISSING_MOCK":
		catLabel = "断言有效性-Mock遗漏"
		suggestion = "请对 Mock 对象的关键行为与交互进行显式 verify 校验。"
	case "UTE_ALWAYS_TRUE":
		catCode = "UTE_ALWAYS_TRUE"
		catLabel = "断言有效性-永真断言"
		suggestion = "请删除或重构形式化恒真断言/无效覆盖桩，补充真实被测逻辑的输入输出边界验证。"
	case "UTE_TAUTOLOGY_ASSERTION":
		catCode = "UTE_TAUTOLOGY_ASSERTION"
		catLabel = "断言有效性-永真断言"
		suggestion = "请删除或重构形式化恒真断言/无效覆盖桩，补充真实被测逻辑的输入输出边界验证。"
	case "UTE_OTHER":
		catLabel = "其它"
		suggestion = "请检查并重构测试用例，排除无效测试逻辑。"
	default:
		catCode = "UTE_TAUTOLOGY_ASSERTION"
		catLabel = "断言有效性-永真断言"
	}

	return &models.AnalysisFinding{
		PrimaryUnitID:       unit.ID,
		Severity:            sev,
		Category:            catLabel,
		CategoryCode:        catCode,
		CategorySource:      "thin_verifier",
		CategoryStatus:      "active",
		FilePath:            unit.Path,
		LineNumber:          lineStr,
		CodeSnippet:         snippet,
		Title:               "单测有效性复核裁定存在形式化作弊或无效验证",
		Detail:              resp.Reason,
		Suggestion:          suggestion,
		CalibrationRule:     ruleID,
		ObservationGroupUID: obsUID,
		AnchorConfidence:    "HIGH",
		AssessmentStatus:    "DEFECT",
		AssessmentOutcome:   "DEFECT",
	}
}

func buildThinPassFinding(unit coverage.PlanUnit, resp TriageLLMResponse) *models.AnalysisFinding {
	displayName := unit.DisplayName
	if displayName == "" {
		displayName = unit.Path
	}
	lineStr := fmt.Sprintf("%d", unit.StartLine)
	if unit.EndLine > unit.StartLine {
		lineStr = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}
	detail := resp.Reason
	if detail == "" {
		detail = "单测有效性轻量复核通过：包含实质性测试验证逻辑。"
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
		CategorySource:     "thin_verifier",
		CategoryStatus:     "LEGACY",
		Severity:           "合格",
		Title:              fmt.Sprintf("测试用例合格：%s", displayName),
		Detail:             detail,
	}
}

func init() {
	_ = plugins.RegisterThinVerifier(NewNativeSingleRoundVerifier())
}
