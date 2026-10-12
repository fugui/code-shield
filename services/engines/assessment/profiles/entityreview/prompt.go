package entityreview

import (
	"fmt"
	"os"
	"strings"

	"code-shield/services/engines/assessment"
)

const maxDomainPromptBytes = 512 * 1024

func (Profile) BuildPrompt(ctx assessment.AssessmentContext, bundle assessment.Bundle) (string, *assessment.PromptRefSession, error) {
	unitIDs := make([]string, 0, len(bundle.Units))
	displayNames := make(map[string]string, len(bundle.Units))
	for _, unit := range bundle.Units {
		unitIDs = append(unitIDs, unit.ID)
		displayNames[unit.ID] = unit.DisplayName
	}
	session, err := assessment.NewPromptRefSession(bundle.ID, unitIDs, displayNames)
	if err != nil {
		return "", nil, err
	}

	var sb strings.Builder
	sb.WriteString("# Role\n你是一个专精于【测试用例有效性评估】的资深代码审计专家。\n\n")
	sb.WriteString("## Invocation Scope\n本次调用只评估 Primary Units；可以读取 Target Files 和相关上下文，但不得遗漏或新增 primary unit。\n\n")
	sb.WriteString("## 评估宗旨\n本次审计是对每一个分配的测试用例出具客观、严谨的质量体检报告：\n")
	sb.WriteString("- **客观公正**：测试用例“合格 (pass)”与“存在缺陷 (defect)”是平等的正确评估结果，严禁为了输出缺陷而过度苛责正常合规的测试。\n")
	sb.WriteString("- **全量覆盖**：每个 Primary Unit 必须且只能输出一个 assessment 结果，无论合格还是有问题。\n\n")
	if ctx.EngineContext != nil && (ctx.EngineContext.AnalysisPromptContent != "" || ctx.EngineContext.AnalysisPromptPath != "") {
		var domainRules []byte
		if ctx.EngineContext.AnalysisPromptContent != "" {
			domainRules = []byte(ctx.EngineContext.AnalysisPromptContent)
		} else {
			domainRules, err = os.ReadFile(ctx.EngineContext.AnalysisPromptPath)
		}
		if err == nil && len(domainRules) > 0 {
			content := assessment.SanitizeDomainPrompt(string(domainRules))
			if len(content) > maxDomainPromptBytes {
				content = content[:maxDomainPromptBytes] + "\n\n[... 领域规则过长，已截断 ...]"
			}
			if len(content) > 0 {
				sb.WriteString("## Domain Specifications\n")
				sb.WriteString(strings.TrimSpace(content))
				sb.WriteString("\n\n")
			}
		}
	}
	sb.WriteString("## Target Files\n")
	for _, path := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", path))
	}
	sb.WriteString("\n## Primary Units\n")
	for _, promptRef := range session.Refs {
		name := strings.TrimSpace(promptRef.DisplayName)
		if name == "" {
			name = "unknown test entity"
		}
		sb.WriteString(fmt.Sprintf("- %s | %s\n", promptRef.Ref, name))
	}
	sb.WriteString("\n每个 Primary Unit 必须且只能输出一个 assessment；只使用分配的 unit_ref，禁止输出内部稳定 ID。\n\n")
	contract, contractErr := renderContract(ctx.AllowedCategories)
	if contractErr != nil {
		return "", nil, contractErr
	}
	sb.WriteString(contract)
	return sb.String(), session, nil
}

func renderContract(allowedCategories []string) (string, error) {
	contract := Profile{}.Contract()
	contract.AllowedCategories = sanitizeAllowedCategories(allowedCategories)
	artifactContract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, contract, "")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(artifactContract.Rendered)
	sb.WriteString("\n\n## Profile Validation Rules\n")
	sb.WriteString("- `defect` 必须携带至少一个 issue；\n")
	sb.WriteString("- `pass`、`not_target`、`needs_human` 严禁携带任何 issues。\n\n")
	sb.WriteString("## 评估报告输出规范\n")
	sb.WriteString("1. 当 `outcome: \"pass\"`（测试用例合格）：必须填写 `summary` 字段（1-2 句话客观概述测试意图、断言有效性与防护价值，作为合格评估报告正文），严禁携带 issues；\n")
	sb.WriteString("2. 当 `outcome: \"defect\"`（测试用例存在缺陷）：必须在 `issues` 数组中记录缺陷（包含 category、severity、detail 与 suggestion）；\n")
	sb.WriteString("3. 当 `outcome: \"not_target\"` 或 `\"needs_human\"`：严禁携带 issues，必须在 `reason` 字段中说明原因。\n")
	sb.WriteString("\n")
	return sb.String(), nil
}

func sanitizeAllowedCategories(categories []string) []string {
	if len(categories) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(categories))
	for _, cat := range categories {
		if cat != "无问题" && cat != "UTE_NO_ISSUE" {
			filtered = append(filtered, cat)
		}
	}
	return filtered
}
