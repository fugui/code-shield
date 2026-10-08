package debate

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/services/invoker"
)

func TestValidateHunterOutput(t *testing.T) {
	out := &HunterOutput{Candidates: []HunterCandidate{
		{CandidateID: "H-001", FilePath: "src/a.cpp", LineRange: "1-2", TriggerLine: "x", ScopeSymbol: "A", Title: "x", CodeSnippet: "x", TriggerCondition: "x", Category: "测试分类"},
		{CandidateID: "H-001", FilePath: "src/a.cpp", LineRange: "1-2", TriggerLine: "x", ScopeSymbol: "A", Title: "x", CodeSnippet: "x", TriggerCondition: "x", Category: "测试分类"},
	}}
	err := validateHunterOutput(out, []string{"测试分类"})
	if err == nil || !strings.Contains(err.Error(), "duplicate candidate_id") {
		t.Fatalf("expected duplicate candidate_id error, got %v", err)
	}
}

func TestDecodeContractJSONCoercesNumericTriggerLine(t *testing.T) {
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "src/a.cpp",
      "line_range": "770-780",
      "trigger_line": 773,
      "scope_symbol": "A::B",
      "category": "测试分类",
      "title": "x",
      "code_snippet": "x",
      "trigger_condition": "x"
    }
  ]
}`
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("ContractForStage failed: %v", err)
	}

	var out HunterOutput
	if err := decodeContractJSON(raw, contract, &out); err != nil {
		t.Fatalf("decodeContractJSON failed: %v", err)
	}
	if out.Candidates[0].TriggerLine != "773" {
		t.Fatalf("TriggerLine = %q, want numeric value coerced to string", out.Candidates[0].TriggerLine)
	}
}

func TestDecodeContractJSONClassifiesTypeMismatch(t *testing.T) {
	raw := `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":"not-an-array"}`
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatalf("ContractForStage failed: %v", err)
	}
	var out HunterOutput
	err = decodeContractJSON(raw, contract, &out)
	if err == nil {
		t.Fatal("decodeContractJSON error = nil, want type mismatch")
	}
	if invoker.ClassifyError(err) != invoker.ErrorClassContractMismatch {
		t.Fatalf("ClassifyError = %q, want contract_mismatch", invoker.ClassifyError(err))
	}
}

func TestNormalizeNumericHunterTriggersReadsPhysicalSource(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "src", "a.cpp")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	source := "// line 1\n// line 2\nif (value > threshold) {\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	out := &HunterOutput{Candidates: []HunterCandidate{{
		FilePath:    "src/a.cpp",
		LineRange:   "2-3",
		TriggerLine: "3",
	}}}
	normalizeNumericHunterTriggers(tempDir, out)
	if out.Candidates[0].TriggerLine != "if (value > threshold) {" {
		t.Fatalf("TriggerLine = %q, want physical source line", out.Candidates[0].TriggerLine)
	}
}

func TestValidateChallengerOutput(t *testing.T) {
	out := &ChallengerOutput{
		DefenseCases: []ChallengerDefenseCase{
			{
				CandidateID:    "H-001",
				DefenseVerdict: "DEFENSE_SUCCESSFUL",
				DefenseArguments: []DefenseArgument{
					{
						Dimension: "Guards",
						Finding:   "source evidence",
						Evidence: []JudgeEvidenceRef{{
							Kind:      JudgeEvidenceCaller,
							Path:      "src/a.cpp",
							LineRange: "1-2",
							Snippet:   "source",
							Reason:    "reachable guard",
						}},
					},
				},
				MitigatingFactors: "guarded",
			},
		},
	}
	if err := validateChallengerOutput(out, []string{"H-001"}, []string{"Guards"}); err != nil {
		t.Fatalf("valid challenger output rejected: %v", err)
	}

	out.DefenseCases[0].DefenseArguments[0].Dimension = "Unknown"
	if err := validateChallengerOutput(out, []string{"H-001"}, []string{"Guards"}); err == nil {
		t.Fatalf("expected unknown dimension to be rejected")
	}
}

func TestValidateChallengerOutputRequiresExactCandidateSet(t *testing.T) {
	out := &ChallengerOutput{}
	err := validateChallengerOutput(out, []string{"H-001"}, nil)
	if err == nil || !strings.Contains(err.Error(), "expected 1 defense_cases") {
		t.Fatalf("expected exact set mismatch, got %v", err)
	}
}

func TestValidateJudgeOutput(t *testing.T) {
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{
		{
			CandidateID:         "H-001",
			Verdict:             "CONFIRMED",
			SeverityPreliminary: "严重",
			Category:            "测试分类",
			FilePath:            "src/a.cpp",
			LineRange:           "1-2",
			TriggerLine:         "x",
			ScopeSymbol:         "A",
			Title:               "x",
			JudgementRationale:  "x",
			CodeSnippet:         "x",
			Suggestion:          "x",
			Evidence: []JudgeEvidenceRef{
				{
					Kind:      JudgeEvidenceCaller,
					Path:      "src/a.cpp",
					LineRange: "1-2",
					Snippet:   "x",
					Reason:    "reachable call site",
				},
			},
		},
	}}
	if err := validateJudgeOutput(out, []string{"H-001"}, []string{"测试分类"}, models.CategoryTaxonomy{}); err != nil {
		t.Fatalf("valid judge output rejected: %v", err)
	}

	out.FinalVerdicts[0].Verdict = "MAYBE"
	if err := validateJudgeOutput(out, []string{"H-001"}, []string{"测试分类"}, models.CategoryTaxonomy{}); err == nil {
		t.Fatalf("expected invalid verdict to be rejected")
	}
}
