package changereview

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
	"path/filepath"
)

func TestBuildPromptPrefersAnalysisPromptContent(t *testing.T) {
	prompt, _, err := Profile{}.BuildPrompt(assessment.AssessmentContext{
		BundleID: "change-001",
		EngineContext: &engines.EngineContext{
			AnalysisPromptContent: "change domain rule",
			AnalysisPromptPath:    filepath.Join(t.TempDir(), "missing.md"),
		},
	}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "change domain rule") {
		t.Fatalf("analysis prompt content was not used:\n%s", prompt)
	}
}

func testBundle() assessment.Bundle {
	return assessment.Bundle{
		ID: "change-001",
		Units: []coverage.PlanUnit{
			{ID: "src/a.cpp#h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/a.cpp", StartLine: 12, EndLine: 18, DisplayName: "src/a.cpp:12-18", Evidence: json.RawMessage(`{"anchor":{"path":"src/a.cpp"}}`)},
			{ID: "src/b.cpp#h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/b.cpp", StartLine: 24, DisplayName: "src/b.cpp:24", Evidence: json.RawMessage(`{"anchor":{"path":"src/b.cpp"}}`)},
		},
		AllFiles: []string{"src/a.cpp", "src/b.cpp"},
	}
}

func testSession(t *testing.T) *assessment.PromptRefSession {
	t.Helper()
	session, err := assessment.NewPromptRefSession("change-001", []string{"src/a.cpp#h1", "src/b.cpp#h1"}, map[string]string{
		"src/a.cpp#h1": "src/a.cpp:12-18",
		"src/b.cpp#h1": "src/b.cpp:24",
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func testPlan(t *testing.T) assessment.PlanView {
	t.Helper()
	return assessment.PlanView{
		BundleID:          "change-001",
		Units:             testBundle().Units,
		AllowedCategories: []string{"无问题", "内存安全"},
		PromptRefs:        testSession(t),
	}
}

func TestBuildPromptUsesShortRefsAndEvidencePack(t *testing.T) {
	prompt, session, err := Profile{}.BuildPrompt(assessment.AssessmentContext{BundleID: "change-001"}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if session == nil || len(session.Refs) != 2 {
		t.Fatalf("unexpected session: %+v", session)
	}
	if strings.Contains(prompt, "sha256:") || strings.Contains(prompt, "src/a.cpp#h1") {
		t.Fatalf("prompt leaked internal primary unit id:\n%s", prompt)
	}
	if !strings.Contains(prompt, "u001 | src/a.cpp:12-18") ||
		!strings.Contains(prompt, "u002 | src/b.cpp:24") ||
		!strings.Contains(prompt, `"path": "src/a.cpp"`) {
		t.Fatalf("prompt missing refs or evidence:\n%s", prompt)
	}
	contract := Profile{}.Contract()
	artifactContract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, contract, "")
	if err != nil {
		t.Fatalf("ArtifactContractForOutput() error = %v", err)
	}
	if !strings.Contains(prompt, artifactContract.Rendered) || strings.Contains(prompt, "结构示例") {
		t.Fatal("prompt does not use the shared artifact contract rendering")
	}
}

func TestNormalizeMapsPromptRefsAndSalvagesUnknownUnits(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass","summary":"safe"},
		{"unit_ref":"u999","outcome":"pass","summary":"unknown"}
	]}`
	artifact, err := Profile{}.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if artifact.Assessments[0].PrimaryUnitID != "src/a.cpp#h1" {
		t.Fatalf("known unit resolved to %q", artifact.Assessments[0].PrimaryUnitID)
	}
	if artifact.Assessments[1].PrimaryUnitID != "" {
		t.Fatalf("unknown unit resolved to %q, want empty", artifact.Assessments[1].PrimaryUnitID)
	}
}

func TestValidateRequiresChangeDomainVerdict(t *testing.T) {
	tests := []struct {
		name    string
		domain  map[string]json.RawMessage
		wantErr string
	}{
		{name: "missing verdict", wantErr: "requires domain.change_verdict"},
		{name: "missing commit", domain: map[string]json.RawMessage{"change_verdict": json.RawMessage(`"REGRESSION"`)}, wantErr: "requires domain.introduced_by_commit"},
		{name: "bad verdict", domain: map[string]json.RawMessage{"change_verdict": json.RawMessage(`"UNKNOWN"`)}, wantErr: "is not allowed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := assessment.UnitAssessment{
				UnitRef:       "u001",
				PrimaryUnitID: "src/a.cpp#h1",
				Outcome:       assessment.OutcomeDefect,
				Summary:       "unsafe",
				Domain:        test.domain,
				Issues:        []assessment.AssessmentIssue{{Category: "内存安全", Severity: "严重", Detail: "null deref"}},
			}
			artifact := assessment.AssessmentArtifact{Schema: ArtifactSchemaV2, Assessments: []assessment.UnitAssessment{item}}
			result, err := Profile{}.Validate(testPlan(t), artifact)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Valid) != 0 || len(result.Invalid) != 1 {
				t.Fatalf("unexpected result: valid=%d invalid=%d", len(result.Valid), len(result.Invalid))
			}
			if got := result.Invalid[0].Errors[0].Detail; !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want contains %q", got, test.wantErr)
			}
		})
	}
}

func TestValidateRequiresNotTargetReason(t *testing.T) {
	artifact := assessment.AssessmentArtifact{Schema: ArtifactSchemaV2, Assessments: []assessment.UnitAssessment{{
		UnitRef:       "u001",
		PrimaryUnitID: "src/a.cpp#h1",
		Outcome:       assessment.OutcomeNotTarget,
		Summary:       "outside change",
	}}}
	result, err := Profile{}.Validate(testPlan(t), artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Valid) != 0 || len(result.Invalid) != 1 || !strings.Contains(result.Invalid[0].Errors[0].Detail, "requires reason") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestValidateSalvagesValidUnitsAndReconcilesPartially(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"not_target","summary":"outside change","reason":"not changed here"},
		{"unit_ref":"u002","outcome":"defect","summary":"unsafe call","domain":{"change_verdict":"EXPOSED_EXISTING"},"issues":[{"category":"内存安全","severity":"严重","detail":"null deref"}]},
		{"unit_ref":"u002","outcome":"pass","summary":"duplicate"},
		{"unit_ref":"u002","outcome":"pass","summary":"invalid because safe has issue","issues":[{"category":"内存安全","severity":"严重","detail":"bad"}]}
	]}`
	artifact, err := Profile{}.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Profile{}.Validate(testPlan(t), artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Valid) != 2 || len(result.Invalid) != 1 || len(result.Duplicate) != 1 {
		t.Fatalf("unexpected result: valid=%d invalid=%d duplicate=%d", len(result.Valid), len(result.Invalid), len(result.Duplicate))
	}
	reconciliation := Profile{}.Reconcile(testPlan(t), result)
	if reconciliation.PlannedUnits != 2 || reconciliation.MatchedUnits != 2 || len(reconciliation.MissingUnits) != 0 {
		t.Fatalf("unexpected reconciliation: %+v", reconciliation)
	}
}

func TestMapFindingsUsesReadableTitlesAndVerdict(t *testing.T) {
	result := assessment.AssessmentResult{Valid: []assessment.UnitAssessment{
		{UnitRef: "u001", PrimaryUnitID: "src/a.cpp#h1", Outcome: assessment.OutcomePass, Summary: "safe"},
		{UnitRef: "u003", PrimaryUnitID: "src/a.cpp#h1", Outcome: assessment.OutcomeNotTarget, Summary: "outside change", Reason: "unrelated"},
		{UnitRef: "u002", PrimaryUnitID: "src/b.cpp#h1", Outcome: assessment.OutcomeDefect, Summary: "unsafe", Domain: map[string]json.RawMessage{"change_verdict": json.RawMessage(`"REGRESSION"`)}, Issues: []assessment.AssessmentIssue{{Category: "内存安全", Severity: "严重", Detail: "null deref", Suggestion: "guard it"}}},
	}}
	findings, err := Profile{}.MapFindings(assessment.AssessmentContext{}, testBundle(), result)
	if err != nil {
		t.Fatalf("MapFindings() error = %v", err)
	}
	want := []string{"内存安全：src/b.cpp:24"}
	for i, finding := range findings {
		if finding.Title != want[i] {
			t.Fatalf("findings[%d].Title = %q, want %q", i, finding.Title, want[i])
		}
	}
	if findings[0].JudgeVerdict != DomainRegression {
		t.Fatalf("JudgeVerdict = %q, want %q", findings[0].JudgeVerdict, DomainRegression)
	}
}

func TestOutcomeForStatusSupportsLegacyValues(t *testing.T) {
	cases := map[string]assessment.AssessmentOutcome{
		"SAFE":               assessment.OutcomePass,
		"REGRESSION":         assessment.OutcomeDefect,
		"EXPOSED_EXISTING":   assessment.OutcomeDefect,
		"NOT_CHANGE_RELATED": assessment.OutcomeNotTarget,
		"CONDITIONAL":        assessment.OutcomeNeedsHuman,
		"NEEDS_HUMAN":        assessment.OutcomeNeedsHuman,
	}
	for status, want := range cases {
		got, ok := Profile{}.OutcomeForStatus(status)
		if !ok || got != want {
			t.Fatalf("OutcomeForStatus(%q) = %q, %v; want %q, true", status, got, ok, want)
		}
	}
}

func TestRegistrationImplementsAllRequiredFacets(t *testing.T) {
	profile := Profile{}
	registration := Registration()
	if !reflect.DeepEqual(registration.Descriptor, profile.Descriptor()) ||
		registration.Descriptor.UnitKind != coverage.PlanUnitChangeHunk ||
		registration.Contract.Contract().SchemaID != ArtifactSchemaV2 {
		t.Fatalf("registration boundary mismatch: %+v", registration.Descriptor)
	}
	if registration.Prompt == nil || registration.Normalize == nil || registration.Validate == nil ||
		registration.Reconcile == nil || registration.MapFindings == nil || registration.Outcome == nil {
		t.Fatalf("registration is incomplete: %+v", registration)
	}
}
