package debate

import (
	"strings"
	"testing"

	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
)

func TestOutputProfileForEngineRejectsUnknownMode(t *testing.T) {
	if got, err := OutputProfileForEngine("debate_full"); err != nil || got != OutputProfileCandidates {
		t.Fatalf("OutputProfileForEngine(debate_full) = (%q, %v), want (%q, nil)", got, err, OutputProfileCandidates)
	}
	if _, err := OutputProfileForEngine("chunked_fast"); err == nil {
		t.Fatal("unknown engine mode must fail")
	}
}

func TestContractForStage(t *testing.T) {
	tests := []struct {
		mode     string
		stage    string
		topLevel string
	}{
		{mode: "debate_full", stage: "hunter", topLevel: "candidates"},
		{mode: "debate_full", stage: "challenger", topLevel: "defense_cases"},
		{mode: "debate_full", stage: "judge", topLevel: "final_verdicts"},
	}
	for _, tt := range tests {
		contract, err := ContractForStage(tt.mode, tt.stage, []string{"测试分类"})
		if err != nil {
			t.Fatalf("ContractForStage(%q, %q) failed: %v", tt.mode, tt.stage, err)
		}
		if contract.TopLevel != tt.topLevel {
			t.Fatalf("ContractForStage(%q, %q) top-level=%q, want %q", tt.mode, tt.stage, contract.TopLevel, tt.topLevel)
		}
		if contract.SchemaID == "" {
			t.Fatalf("expected schema id for %s/%s", tt.mode, tt.stage)
		}
	}
}

func TestJudgeContractUsesEvidenceV2(t *testing.T) {
	contract, err := ContractForStage("debate_full", "judge", []string{"测试分类"})
	if err != nil {
		t.Fatalf("ContractForStage failed: %v", err)
	}
	if contract.SchemaID != FinalVerdictsArtifactSchemaV2 {
		t.Fatalf("judge schema=%q, want %q", contract.SchemaID, FinalVerdictsArtifactSchemaV2)
	}
	rendered := RenderOutputContract(contract)
	for _, want := range []string{
		FinalVerdictsArtifactSchemaV2,
		"`evidence[].kind`",
		"禁止返回 `source_verified`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered judge contract missing %q:\n%s", want, rendered)
		}
	}
}

func TestChallengerContractUsesEvidenceV2(t *testing.T) {
	contract, err := ContractForStage("debate_full", "challenger", []string{"测试分类"})
	if err != nil {
		t.Fatalf("ContractForStage failed: %v", err)
	}
	if contract.SchemaID != DefenseCasesArtifactSchemaV2 {
		t.Fatalf("challenger schema=%q, want %q", contract.SchemaID, DefenseCasesArtifactSchemaV2)
	}
	rendered := RenderOutputContract(contract)
	for _, want := range []string{
		DefenseCasesArtifactSchemaV2,
		"`defense_arguments[].evidence[].kind`",
		"禁止返回 `source_verified`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered challenger contract missing %q:\n%s", want, rendered)
		}
	}
}

func TestUnknownEngineModeContractFails(t *testing.T) {
	if _, err := ContractForStage("chunked_fast", "analysis", nil); err == nil {
		t.Fatal("unknown engine mode must fail")
	}
}

func TestPromptAssemblerEngineOutputContracts(t *testing.T) {
	assembler := &PromptAssembler{}
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/main.cpp"}}

	prompt := assembler.BuildHunterPrompt(&engines.EngineContext{EngineMode: "chunked_fast"}, bundle)
	if prompt != "" {
		t.Fatalf("unknown engine mode must not render prompt: %q", prompt)
	}

	debateCtx := &engines.EngineContext{EngineMode: "debate_full", AllowedCategories: []string{"并发安全"}}
	debatePrompt := assembler.BuildHunterPrompt(debateCtx, bundle)
	if !strings.Contains(debatePrompt, CandidatesArtifactSchemaV1) || !strings.Contains(debatePrompt, `"candidates"`) {
		t.Fatalf("debate prompt does not contain candidates contract:\n%s", debatePrompt)
	}
	if strings.Contains(debatePrompt, `"findings"`) {
		t.Fatalf("debate prompt must not contain findings contract:\n%s", debatePrompt)
	}
}
