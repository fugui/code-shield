package debate

import (
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines/assessment"
	"code-shield/services/invoker"
)

func TestGenericAssessmentContractRetryDisabledWhenContractPipelineEnabled(t *testing.T) {
	enabled := true
	previous := models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = previous })

	err := invoker.NewClassifiedError(invoker.ErrorClassContractMismatch, "assessment artifact invalid")
	if shouldRunGenericAssessmentContractRetry(err) {
		t.Fatal("generic contract retry should be disabled when directed repair is enabled")
	}
}

func TestBuildDirectedAssessmentRepairPromptIsStable(t *testing.T) {
	request := assessment.DirectedRepairRequest{
		RawArtifact: `{}`,
		Contract: assessment.ArtifactContract{
			SchemaID: assessment.AssessmentsArtifactSchemaV2,
			JSONSchema: map[string]any{
				"type": "object",
			},
		},
		ValidRefs: []assessment.PromptRef{{Ref: "u001", UnitID: "unit-1", DisplayName: "Unit One"}},
		AllowedEnums: map[string][]string{
			"outcome":           {"pass", "defect"},
			"issues[].category": {"内存安全"},
		},
	}
	first := buildDirectedAssessmentRepairPrompt(request)
	second := buildDirectedAssessmentRepairPrompt(request)
	if first != second {
		t.Fatal("repair prompt changed between calls")
	}
	if !strings.Contains(first, "- issues[].category: 内存安全\n- outcome: pass, defect\n") {
		t.Fatalf("allowed enums are not deterministically ordered:\n%s", first)
	}
}

func TestAssessmentJSONSchemaRequestFollowsMode(t *testing.T) {
	contract := assessment.ArtifactContract{
		SchemaID: assessment.AssessmentsArtifactSchemaV2,
		JSONSchema: map[string]any{
			"type": "object",
		},
	}
	if assessmentJSONSchemaRequest(contract, "off") != nil {
		t.Fatal("off mode should not request json_schema")
	}
	schema := assessmentJSONSchemaRequest(contract, "auto")
	if schema == nil || schema.Name != "code-shield_unit-assessments_v2" || schema.Strict {
		t.Fatalf("schema request = %#v", schema)
	}
	strict := assessmentJSONSchemaRequest(contract, "strict")
	if strict == nil || !strict.Strict {
		t.Fatalf("strict request = %#v", strict)
	}
}
