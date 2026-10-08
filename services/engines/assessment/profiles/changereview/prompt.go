package changereview

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"code-shield/services/coverage"
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
	sb.WriteString("# Role\n你是一个专精于【变更回归检视】的资深代码审计专家。\n\n")
	sb.WriteString("## Invocation Scope\n本次调用只评估 Primary Units；可以读取 Target Files 和 Evidence Pack，但不得遗漏或新增 primary unit。\n\n")
	if ctx.EngineContext != nil && (ctx.EngineContext.AnalysisPromptContent != "" || ctx.EngineContext.AnalysisPromptPath != "") {
		var domainRules []byte
		if ctx.EngineContext.AnalysisPromptContent != "" {
			domainRules = []byte(ctx.EngineContext.AnalysisPromptContent)
		} else {
			domainRules, err = os.ReadFile(ctx.EngineContext.AnalysisPromptPath)
		}
		if err == nil && len(domainRules) > 0 {
			content := string(domainRules)
			if len(content) > maxDomainPromptBytes {
				content = content[:maxDomainPromptBytes] + "\n\n[... 领域规则过长，已截断 ...]"
			}
			sb.WriteString("## Domain Specifications\n")
			sb.WriteString(strings.TrimSpace(content))
			sb.WriteString("\n\n")
		}
	}
	sb.WriteString("## Target Files\n")
	for _, path := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", path))
	}
	sb.WriteString("\n## Primary Units\n")
	for _, promptRef := range session.Refs {
		sb.WriteString(fmt.Sprintf("- %s | %s\n", promptRef.Ref, displayValue(promptRef.DisplayName)))
	}
	if err := appendEvidencePack(&sb, session, bundle.Units); err != nil {
		return "", nil, err
	}
	sb.WriteString("\n每个 Primary Unit 必须且只能输出一个 assessment；只使用分配的 unit_ref，禁止输出内部 primary_unit_id。\n\n")
	contract, contractErr := renderContract(ctx.AllowedCategories)
	if contractErr != nil {
		return "", nil, contractErr
	}
	sb.WriteString(contract)
	return sb.String(), session, nil
}

func appendEvidencePack(sb *strings.Builder, session *assessment.PromptRefSession, units []coverage.PlanUnit) error {
	sb.WriteString("\n## Evidence Pack\n")
	for _, ref := range session.Refs {
		var unit coverage.PlanUnit
		for _, candidate := range units {
			if candidate.ID == ref.UnitID {
				unit = candidate
				break
			}
		}
		var evidence json.RawMessage
		if len(unit.Evidence) > 0 {
			if err := json.Unmarshal(unit.Evidence, &evidence); err != nil {
				return fmt.Errorf("decode evidence for primary unit %q: %w", unit.ID, err)
			}
		} else {
			evidence = json.RawMessage(`{}`)
		}
		encoded, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return fmt.Errorf("encode evidence for primary unit %q: %w", unit.ID, err)
		}
		sb.WriteString(fmt.Sprintf("\n### %s\n```json\n%s\n```\n", ref.Ref, encoded))
	}
	return nil
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown change hunk"
	}
	return value
}

func renderContract(allowedCategories []string) (string, error) {
	contract := Profile{}.Contract()
	contract.AllowedCategories = append([]string(nil), allowedCategories...)
	artifactContract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, contract, "")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(artifactContract.Rendered)
	sb.WriteString("\n\n## Profile Validation Rules\n")
	sb.WriteString("- `summary` 必填，说明回归判定理由。\n")
	sb.WriteString("- `domain.change_verdict` 只允许 `REGRESSION`、`EXPOSED_EXISTING`、`CONDITIONAL`。\n")
	sb.WriteString("- `domain.change_verdict=REGRESSION` 必须提供 `domain.introduced_by_commit`。\n")
	sb.WriteString("- `not_target` 必须提供 `reason`。\n")
	sb.WriteString("- `defect` 必须携带至少一个 issue；`pass` / `not_target` 不得携带 issues。\n")
	sb.WriteString("\n")
	return sb.String(), nil
}
