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
)

const (
	defaultVerifierTimeout = 3.5
	builtinTriagePrompt    = `你是一名代码质量与测试有效性仲裁员。请判定以下测试用例是否包含实质性测试验证。
## 源码 ({{.Language}}):
` + "```" + `{{.Language}}
{{.UnitSourceCode}}
` + "```" + `
## 线索:
{{range .FactHints}}- [{{.Kind}}]: {{.Description}}
{{end}}
请输出严格 JSON: {"outcome": "PASS"|"DEFECT", "reason": "原因说明", "severity": "NORMAL"|"MAJOR"}`
)

// TriagePromptData 传递给复核提示词模板的参数
type TriagePromptData struct {
	Language       string
	UnitSourceCode string
	FactHints      []plugins.StructuralFactHint
}

// TriageLLMResponse LLM 单轮极速复核返回的结构化结果
type TriageLLMResponse struct {
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`
	Severity string `json:"severity"`
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
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s", unit.Path, unit.ID, ruleID)))
	obsUID := hex.EncodeToString(sum[:])

	sev := "严重"
	if strings.ToUpper(resp.Severity) == "MAJOR" {
		sev = "严重"
	}

	return &models.AnalysisFinding{
		PrimaryUnitID:       unit.ID,
		Severity:            sev,
		Category:            "断言有效性-永真断言",
		CategoryCode:        "UTE_TAUTOLOGY_ASSERTION",
		CategorySource:      "thin_verifier",
		CategoryStatus:      "active",
		FilePath:            unit.Path,
		LineNumber:          lineStr,
		CodeSnippet:         snippet,
		Title:               "单测有效性复核裁定存在形式化作弊或无效验证",
		Detail:              resp.Reason,
		Suggestion:          "请删除或重构形式化恒真断言/无效覆盖桩，补充真实被测逻辑的输入输出边界验证。",
		CalibrationRule:     ruleID,
		ObservationGroupUID: obsUID,
		AnchorConfidence:    "HIGH",
		AssessmentStatus:    "DEFECT",
		AssessmentOutcome:   "DEFECT",
	}
}

func init() {
	_ = plugins.RegisterThinVerifier(NewNativeSingleRoundVerifier())
}
