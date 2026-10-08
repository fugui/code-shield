package occurrencereview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
)

func testBundle() assessment.Bundle {
	return assessment.Bundle{
		ID: "occurrence-001",
		Units: []coverage.PlanUnit{
			{ID: "sha256:one", Kind: coverage.PlanUnitKeywordOccurrence, Path: "src/a.cpp", StartLine: 42, DisplayName: "pthread_create @ src/a.cpp:42"},
			{ID: "sha256:two", Kind: coverage.PlanUnitKeywordOccurrence, Path: "src/b.cpp", StartLine: 18, DisplayName: "pthread_create @ src/b.cpp:18"},
		},
		AllFiles: []string{"src/a.cpp", "src/b.cpp"},
	}
}

func TestBuildPromptPrefersAnalysisPromptContent(t *testing.T) {
	prompt, _, err := Profile{}.BuildPrompt(assessment.AssessmentContext{
		BundleID: "occurrence-001",
		EngineContext: &engines.EngineContext{
			AnalysisPromptContent: "occurrence domain rule",
			AnalysisPromptPath:    filepath.Join(t.TempDir(), "missing.md"),
		},
	}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "occurrence domain rule") {
		t.Fatalf("analysis prompt content was not used:\n%s", prompt)
	}
}

func testSession(t *testing.T) *assessment.PromptRefSession {
	t.Helper()
	bundle := testBundle()
	session, err := assessment.NewPromptRefSession(bundle.ID, []string{"sha256:one", "sha256:two"}, map[string]string{
		"sha256:one": "pthread_create @ src/a.cpp:42",
		"sha256:two": "pthread_create @ src/b.cpp:18",
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestBuildPromptUsesShortRefsWithoutHashes(t *testing.T) {
	prompt, session, err := Profile{}.BuildPrompt(assessment.AssessmentContext{
		BundleID:          "occurrence-001",
		AllowedCategories: []string{"无问题", "生命周期-join遗漏"},
	}, testBundle())
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if prompt == "" || session == nil || len(session.Refs) != 2 {
		t.Fatalf("unexpected prompt/session: %q %+v", prompt, session)
	}
	if strings.Contains(prompt, "sha256:") {
		t.Fatal("prompt leaked internal primary unit hashes")
	}
	if !strings.Contains(prompt, "u001 | pthread_create @ src/a.cpp:42") ||
		!strings.Contains(prompt, "u002 | pthread_create @ src/b.cpp:18") {
		t.Fatalf("prompt missing readable unit list:\n%s", prompt)
	}
	artifactContract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, Profile{}.Contract(), "")
	if err != nil {
		t.Fatalf("ArtifactContractForOutput() error = %v", err)
	}
	if !strings.Contains(prompt, artifactContract.Rendered) || strings.Contains(prompt, "结构示例") {
		t.Fatal("prompt does not use the shared artifact contract rendering")
	}
}

func TestNormalizeMapsPromptRefs(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass","summary":"safe"},
		{"unit_ref":"u002","outcome":"not_target","reason":"comment"}
	]}`
	artifact, err := Profile{}.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if artifact.Assessments[0].PrimaryUnitID != "sha256:one" || artifact.Assessments[1].PrimaryUnitID != "sha256:two" {
		t.Fatalf("unexpected primary unit mapping: %+v", artifact.Assessments)
	}
}

func TestNormalizeRejectsDuplicateAndUnknownRefs(t *testing.T) {
	duplicate := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass"},
		{"unit_ref":"u001","outcome":"pass"}
	]}`
	profile := Profile{}
	duplicateArtifact, err := profile.Normalize(duplicate, testSession(t))
	if err != nil {
		t.Fatalf("Normalize() duplicate error = %v, want salvageable artifact", err)
	}
	if len(duplicateArtifact.Assessments) != 2 {
		t.Fatalf("Normalize() duplicate kept %d assessments, want 2", len(duplicateArtifact.Assessments))
	}
	unknown := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[{"unit_ref":"u999","outcome":"pass"}]}`
	unknownArtifact, err := profile.Normalize(unknown, testSession(t))
	if err != nil {
		t.Fatalf("Normalize() unknown error = %v, want salvageable artifact", err)
	}
	if unknownArtifact.Assessments[0].PrimaryUnitID != "" {
		t.Fatalf("Normalize() unknown resolved to %q, want empty", unknownArtifact.Assessments[0].PrimaryUnitID)
	}
}

func TestValidateSalvagesValidUnits(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"pass"},
		{"unit_ref":"u002","outcome":"defect","issues":[{"category":"生命周期-join遗漏","severity":"严重","detail":"no join"}]},
		{"unit_ref":"u002","outcome":"pass"}
	]}`
	artifact, err := Profile{}.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatal(err)
	}
	plan := assessment.PlanView{
		BundleID:          "occurrence-001",
		Units:             testBundle().Units,
		AllowedCategories: []string{"无问题", "生命周期-join遗漏"},
		PromptRefs:        testSession(t),
	}
	result, err := Profile{}.Validate(plan, artifact)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(result.Valid) != 2 || len(result.Invalid) != 0 || len(result.Missing) != 0 || len(result.Duplicate) != 1 {
		t.Fatalf("unexpected result: valid=%d invalid=%d missing=%v duplicate=%v",
			len(result.Valid), len(result.Invalid), result.Missing, result.Duplicate)
	}
	reconciliation := Profile{}.Reconcile(plan, result)
	if reconciliation.PlannedUnits != 2 || reconciliation.MatchedUnits != 2 {
		t.Fatalf("unexpected reconciliation: %+v", reconciliation.PlanReconciliation)
	}
}

func TestValidateRejectsIssueCategoryOutsideWhitelist(t *testing.T) {
	raw := `{"schema":"` + ArtifactSchemaV2 + `","assessments":[
		{"unit_ref":"u001","outcome":"defect","issues":[{"category":"未授权","severity":"严重","detail":"no join"}]}
	]}`
	artifact, err := Profile{}.Normalize(raw, testSession(t))
	if err != nil {
		t.Fatal(err)
	}
	plan := assessment.PlanView{
		Units:             testBundle().Units,
		AllowedCategories: []string{"无问题", "生命周期-join遗漏"},
		PromptRefs:        testSession(t),
	}
	result, err := Profile{}.Validate(plan, artifact)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(result.Valid) != 0 || len(result.Invalid) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestMapFindingsUsesReadableTitle(t *testing.T) {
	result := assessment.AssessmentResult{Valid: []assessment.UnitAssessment{
		{UnitRef: "u001", PrimaryUnitID: "sha256:one", Outcome: assessment.OutcomePass},
		{UnitRef: "u002", PrimaryUnitID: "sha256:two", Outcome: assessment.OutcomeNotTarget, Reason: "comment"},
	}}
	findings, err := Profile{}.MapFindings(assessment.AssessmentContext{}, testBundle(), result)
	if err != nil {
		t.Fatalf("MapFindings() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want no pass/not_target findings", findings)
	}
}

func TestMapFindingsUsesTaskSemantics(t *testing.T) {
	result := assessment.AssessmentResult{Valid: []assessment.UnitAssessment{
		{UnitRef: "u001", PrimaryUnitID: "sha256:one", Outcome: assessment.OutcomePass, Summary: "no unordered collection"},
		{UnitRef: "u002", PrimaryUnitID: "sha256:two", Outcome: assessment.OutcomeNotTarget, Reason: "keyword is in a comment"},
		{UnitRef: "u003", PrimaryUnitID: "sha256:three", Outcome: assessment.OutcomeNeedsHuman, Reason: "requires review"},
		{UnitRef: "u004", PrimaryUnitID: "sha256:four", Outcome: assessment.OutcomeDefect, Issues: []assessment.AssessmentIssue{{
			Category: "接口列表顺序不稳定",
			Severity: "一般",
			Detail:   "response order is unstable",
		}}},
	}}
	bundle := testBundle()
	bundle.Units = append(bundle.Units, coverage.PlanUnit{
		ID:          "sha256:three",
		Kind:        coverage.PlanUnitKeywordOccurrence,
		Path:        "src/c.cpp",
		DisplayName: "map @ src/c.cpp:7",
	})
	bundle.Units = append(bundle.Units, coverage.PlanUnit{
		ID:          "sha256:four",
		Kind:        coverage.PlanUnitKeywordOccurrence,
		Path:        "src/d.cpp",
		DisplayName: "unordered_map @ src/d.cpp:12",
	})
	findings, err := Profile{}.MapFindings(assessment.AssessmentContext{
		EngineContext: &engines.EngineContext{
			TargetSemantics: models.TargetSemantics{
				Singular:       "源码文件",
				NotTargetLabel: "非目标文件",
			},
			DisplaySemantics: models.DisplaySemantics{
				TargetLabel:    "源码文件",
				NotTargetLabel: "非目标文件",
			},
		},
	}, bundle, result)
	if err != nil {
		t.Fatalf("MapFindings() error = %v", err)
	}
	want := []string{
		"源码文件需人工评估：map @ src/c.cpp:7",
		"接口列表顺序不稳定：unordered_map @ src/d.cpp:12",
	}
	wantDetails := []string{
		"requires review",
		"[一般/接口列表顺序不稳定] response order is unstable 修复建议：",
	}
	for i, finding := range findings {
		if finding.Title != want[i] || strings.Contains(finding.Title, "sha256:") {
			t.Fatalf("findings[%d].Title = %q, want %q", i, finding.Title, want[i])
		}
		if finding.Detail != wantDetails[i] {
			t.Fatalf("findings[%d].Detail = %q, want %q", i, finding.Detail, wantDetails[i])
		}
	}
}

func TestMapFindingsExtractsOccurrenceSnippet(t *testing.T) {
	codesPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(codesPath, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "void parse(void) {\n  char *json = cJSON_Parse(data);\n}\n"
	if err := os.WriteFile(filepath.Join(codesPath, "src", "a.cpp"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	bundle := assessment.Bundle{
		ID: "occurrence-001",
		Units: []coverage.PlanUnit{{
			ID:          "sha256:one",
			Kind:        coverage.PlanUnitKeywordOccurrence,
			Path:        "src/a.cpp",
			StartLine:   2,
			EndLine:     2,
			DisplayName: "cJSON_ @ src/a.cpp:2",
		}},
	}
	result := assessment.AssessmentResult{Valid: []assessment.UnitAssessment{{
		UnitRef:       "u001",
		PrimaryUnitID: "sha256:one",
		Outcome:       assessment.OutcomeDefect,
		Issues: []assessment.AssessmentIssue{{
			Category: "cJSON 解析结果未释放",
			Severity: "一般",
			Detail:   "parse result is never deleted",
		}},
	}}}
	findings, err := Profile{}.MapFindings(assessment.AssessmentContext{
		EngineContext: &engines.EngineContext{CodesPath: codesPath},
	}, bundle, result)
	if err != nil {
		t.Fatalf("MapFindings() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].LineNumber != "2" {
		t.Fatalf("LineNumber = %q, want 2", findings[0].LineNumber)
	}
	if findings[0].CodeSnippet != "char *json = cJSON_Parse(data);" {
		t.Fatalf("CodeSnippet = %q", findings[0].CodeSnippet)
	}
}

func isJSONStatus(raw []byte, status string) bool {
	var artifact assessment.UnitAssessment
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return false
	}
	return string(artifact.Outcome) == status
}

func TestOutcomeForStatusSupportsLegacyValues(t *testing.T) {
	cases := map[string]assessment.AssessmentOutcome{
		"valid":               assessment.OutcomePass,
		"not_thread_creation": assessment.OutcomeNotTarget,
		"issue":               assessment.OutcomeDefect,
		"needs_human":         assessment.OutcomeNeedsHuman,
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
		profile.Descriptor().UnitKind != coverage.PlanUnitKeywordOccurrence ||
		profile.Contract().SchemaID != ArtifactSchemaV2 {
		t.Fatal("descriptor and contract do not form the occurrence profile boundary")
	}
	if !reflect.TypeOf(profile).Implements(reflect.TypeOf((*assessment.ArtifactNormalizer)(nil)).Elem()) {
		t.Fatal("profile does not implement normalizer facet")
	}
}
