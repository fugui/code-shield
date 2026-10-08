package entityreview

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
)

func TestBuildPromptPrefersAnalysisPromptContent(t *testing.T) {
	profile := Profile{}
	prompt, _, err := profile.BuildPrompt(assessment.AssessmentContext{
		BundleID: "entity-001",
		EngineContext: &engines.EngineContext{
			AnalysisPromptContent: "entity domain rule",
			AnalysisPromptPath:    filepath.Join(t.TempDir(), "missing.md"),
		},
	}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "entity domain rule") {
		t.Fatalf("analysis prompt content was not used:\n%s", prompt)
	}
}

func testBundle() assessment.Bundle {
	return assessment.Bundle{
		ID: "entity-001",
		Units: []coverage.PlanUnit{
			{ID: "sha256:one", Kind: coverage.PlanUnitEntity, Path: "tests/a_test.cpp", StartLine: 18, EndLine: 48, DisplayName: "ThreadPoolTest.Submit_Worker"},
			{ID: "sha256:two", Kind: coverage.PlanUnitEntity, Path: "tests/b_test.cpp", StartLine: 64, EndLine: 82, DisplayName: "ThreadPoolTest.Cancel_Runs_Thread"},
		},
		AllFiles: []string{"tests/a_test.cpp", "tests/b_test.cpp"},
	}
}

func testSession(t *testing.T) *assessment.PromptRefSession {
	t.Helper()
	session, err := assessment.NewPromptRefSession("entity-001", []string{"sha256:one", "sha256:two"}, map[string]string{
		"sha256:one": "ThreadPoolTest.Submit_Worker",
		"sha256:two": "ThreadPoolTest.Cancel_Runs_Thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestBuildPromptUsesShortRefsWithoutHashes(t *testing.T) {
	profile := Profile{}
	prompt, session, err := profile.BuildPrompt(assessment.AssessmentContext{
		BundleID:          "entity-001",
		AllowedCategories: []string{"无问题", "断言有效性-空测试"},
	}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt, "sha256:") {
		t.Fatal("prompt leaked internal primary unit hashes")
	}
	if !strings.Contains(prompt, "u001 | ThreadPoolTest.Submit_Worker") || !strings.Contains(prompt, "u002 | ThreadPoolTest.Cancel_Runs_Thread") {
		t.Fatalf("prompt missing readable units:\n%s", prompt)
	}
	if session == nil || len(session.Refs) != 2 {
		t.Fatalf("unexpected session: %+v", session)
	}
	contract := profile.Contract()
	artifactContract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, contract, "")
	if err != nil {
		t.Fatalf("ArtifactContractForOutput() error = %v", err)
	}
	if !strings.Contains(prompt, artifactContract.Rendered) || strings.Contains(prompt, "结构示例") {
		t.Fatal("prompt does not use the shared artifact contract rendering")
	}
}

func TestNormalizeMapsPromptRefsAndKeepsDuplicates(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass","summary":"has business assertion"},
		{"unit_ref":"u002","outcome":"pass"},
		{"unit_ref":"u002","outcome":"defect"}
	]}`
	profile := Profile{}
	artifact, err := profile.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if artifact.Assessments[0].PrimaryUnitID != "sha256:one" || artifact.Assessments[1].PrimaryUnitID != "sha256:two" {
		t.Fatalf("unexpected mapping: %+v", artifact.Assessments)
	}
}

func TestValidateSalvagesFirstDuplicate(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass"},
		{"unit_ref":"u002","outcome":"defect","issues":[{"category":"断言有效性-空测试","severity":"严重","detail":"no assertion"}]},
		{"unit_ref":"u002","outcome":"pass"}
	]}`
	profile := Profile{}
	artifact, err := profile.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatal(err)
	}
	plan := assessment.PlanView{
		BundleID:          "entity-001",
		Units:             testBundle().Units,
		AllowedCategories: []string{"无问题", "断言有效性-空测试"},
		PromptRefs:        testSession(t),
	}
	result, err := profile.Validate(plan, artifact)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(result.Valid) != 2 || len(result.Duplicate) != 1 || len(result.Missing) != 0 {
		t.Fatalf("unexpected result: valid=%d duplicate=%v missing=%v", len(result.Valid), result.Duplicate, result.Missing)
	}
	reconciliation := profile.Reconcile(plan, result)
	if reconciliation.PlannedUnits != 2 || reconciliation.MatchedUnits != 2 {
		t.Fatalf("unexpected reconciliation: %+v", reconciliation.PlanReconciliation)
	}
}

func TestValidateRejectsWrongUnitKindAndMissingIssue(t *testing.T) {
	profile := Profile{}
	badKind := assessment.PlanView{Units: []coverage.PlanUnit{{ID: "file", Kind: coverage.PlanUnitFile}}, PromptRefs: testSession(t)}
	if _, err := profile.Validate(badKind, assessment.AssessmentArtifact{Schema: ArtifactSchemaV2}); err == nil {
		t.Fatal("Validate() wrong kind = nil, want error")
	}
	plan := assessment.PlanView{
		Units:             testBundle().Units,
		AllowedCategories: []string{"无问题", "断言有效性-空测试"},
		PromptRefs:        testSession(t),
	}
	artifact := assessment.AssessmentArtifact{Schema: ArtifactSchemaV2, Assessments: []assessment.UnitAssessment{
		{UnitRef: "u001", PrimaryUnitID: "sha256:one", Outcome: assessment.OutcomeDefect},
	}}
	result, err := profile.Validate(plan, artifact)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(result.Valid) != 0 || len(result.Invalid) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestMapFindingsUsesReadableTitle(t *testing.T) {
	result := assessment.AssessmentResult{Valid: []assessment.UnitAssessment{
		{UnitRef: "u001", PrimaryUnitID: "sha256:one", Outcome: assessment.OutcomePass, Summary: "has business assertion"},
		{UnitRef: "u002", PrimaryUnitID: "sha256:two", Outcome: assessment.OutcomeDefect, Issues: []assessment.AssessmentIssue{
			{Category: "断言有效性-空测试", Severity: "严重", Detail: "no assertion", Suggestion: "add assertion"},
		}},
	}}
	findings, err := Profile{}.MapFindings(assessment.AssessmentContext{}, testBundle(), result)
	if err != nil {
		t.Fatalf("MapFindings() error = %v", err)
	}
	want := []string{
		"测试用例合格：ThreadPoolTest.Submit_Worker",
		"断言有效性-空测试：ThreadPoolTest.Cancel_Runs_Thread",
	}
	for i, finding := range findings {
		if finding.Title != want[i] || strings.Contains(finding.Title, "sha256:") {
			t.Fatalf("findings[%d].Title = %q, want %q", i, finding.Title, want[i])
		}
	}
}

func TestOutcomeForStatusSupportsLegacyValues(t *testing.T) {
	cases := map[string]assessment.AssessmentOutcome{
		"valid":       assessment.OutcomePass,
		"invalid":     assessment.OutcomeDefect,
		"issue":       assessment.OutcomeDefect,
		"needs_human": assessment.OutcomeNeedsHuman,
	}
	for status, want := range cases {
		got, ok := Profile{}.OutcomeForStatus(status)
		if !ok || got != want {
			t.Fatalf("OutcomeForStatus(%q) = %v, %v; want %v, true", status, got, ok, want)
		}
	}
}

func TestRegistrationImplementsAllRequiredFacets(t *testing.T) {
	profile := Profile{}
	if profile.Descriptor().Name != ProfileName ||
		profile.Descriptor().UnitKind != coverage.PlanUnitEntity ||
		profile.Contract().SchemaID != ArtifactSchemaV2 {
		t.Fatal("descriptor and contract do not form the entity profile boundary")
	}
	if !reflect.TypeOf(profile).Implements(reflect.TypeOf((*assessment.ArtifactValidator)(nil)).Elem()) {
		t.Fatal("profile does not implement validator facet")
	}
}
