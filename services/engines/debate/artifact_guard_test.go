package debate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

func TestNormalizeHunterArtifactAliasesAndMultiRange(t *testing.T) {
	repoRoot := t.TempDir()
	sourceRel := "src/control.cpp"
	if err := os.MkdirAll(filepath.Join(repoRoot, filepath.Dir(sourceRel)), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	source := strings.Repeat("// filler\n", 68) + "if (handler == NULL) {\n"
	if err := os.WriteFile(filepath.Join(repoRoot, sourceRel), []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	absolutePath := filepath.Join(repoRoot, sourceRel)
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "id": "CORE-001",
    "file": "` + absolutePath + `",
    "function": "A::Check",
    "line_range": "59-65, 70-91",
    "trigger_line": "if (handler == NULL) {",
    "category": "测试分类",
    "description": "空指针解引用。需要补齐判空。",
    "snippet": "if (handler == NULL) {"
  }]
}`

	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), repoRoot, []string{sourceRel})
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("normalized JSON is invalid: %v\n%s", err, normalized)
	}
	candidate := out.Candidates[0]
	if candidate.CandidateID != "CORE-001" || candidate.FilePath != sourceRel ||
		candidate.ScopeSymbol != "A::Check" || candidate.LineRange != "59-65" {
		t.Fatalf("unexpected normalized candidate: %+v", candidate)
	}
	if len(candidate.AdditionalLineRanges) != 1 || candidate.AdditionalLineRanges[0] != "70-91" {
		t.Fatalf("additional ranges = %#v", candidate.AdditionalLineRanges)
	}
	if candidate.TriggerCondition == "" || candidate.Title == "" || candidate.CodeSnippet == "" {
		t.Fatalf("derived fields are empty: %+v", candidate)
	}
	codes := map[string]int{}
	for _, issue := range issues {
		codes[issue.Code]++
	}
	for _, code := range []string{
		IssueAliasNormalized, IssuePathNormalized, IssueLineRangeNormalized,
	} {
		if codes[code] == 0 {
			t.Fatalf("expected issue %q, got %#v", code, issues)
		}
	}
}

func TestNormalizeHunterArtifactSelectsNumericPrimary(t *testing.T) {
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-003",
    "file_path": "src/a.cpp",
    "line_range": "404, 70-91",
    "trigger_line": 404,
    "scope_symbol": "A::B",
    "category": "测试分类",
    "title": "x",
    "code_snippet": "x",
    "trigger_condition": "x"
  }]
}`
	normalized, _ := NormalizeHunterArtifactJSON([]byte(raw), "", nil)
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid normalized JSON: %v", err)
	}
	if out.Candidates[0].LineRange != "404" || len(out.Candidates[0].AdditionalLineRanges) != 1 ||
		out.Candidates[0].AdditionalLineRanges[0] != "70-91" {
		t.Fatalf("unexpected line normalization: %+v", out.Candidates[0])
	}
}

func TestNormalizeHunterArtifactRepairsMissingCandidateID(t *testing.T) {
	raw := `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"","file_path":"src/a.cpp","line_range":"1-2","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"x","code_snippet":"x","trigger_condition":"x"}]}`
	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), "", nil)
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Candidates[0].CandidateID != "H-001" {
		t.Fatalf("candidate id = %q", out.Candidates[0].CandidateID)
	}
	if len(issues) == 0 || issues[0].Code != IssueCandidateIDRepaired {
		t.Fatalf("expected candidate id issue, got %#v", issues)
	}
}

func TestNormalizeHunterArtifactKeepsFlatTwoRangeStrings(t *testing.T) {
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001",
    "file_path": "src/a.cpp",
    "line_range": "41-44",
    "additional_line_ranges": ["62-72", "37-41"],
    "trigger_line": "x",
    "scope_symbol": "A",
    "category": "测试分类",
    "title": "x",
    "code_snippet": "x",
    "trigger_condition": "x"
  }]
}`
	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), "", nil)
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid normalized JSON: %v\n%s", err, normalized)
	}
	additional := out.Candidates[0].AdditionalLineRanges
	if len(additional) != 2 || additional[0] != "62-72" || additional[1] != "37-41" {
		t.Fatalf("additional ranges = %#v, want two preserved entries", additional)
	}
	for _, issue := range issues {
		if !issue.Recoverable {
			t.Fatalf("flat two-range strings must not create unresolved issues, got %#v", issues)
		}
	}
}

func TestNormalizeHunterArtifactSplitsSpaceSeparatedRanges(t *testing.T) {
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001",
    "file_path": "src/a.cpp",
    "line_range": "62-72 37-41",
    "additional_line_ranges": ["45 56"],
    "trigger_line": "x",
    "scope_symbol": "A",
    "category": "测试分类",
    "title": "x",
    "code_snippet": "x",
    "trigger_condition": "x"
  }]
}`
	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), "", nil)
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid normalized JSON: %v\n%s", err, normalized)
	}
	candidate := out.Candidates[0]
	if candidate.LineRange != "62-72" || len(candidate.AdditionalLineRanges) != 3 ||
		candidate.AdditionalLineRanges[0] != "37-41" || candidate.AdditionalLineRanges[1] != "45" ||
		candidate.AdditionalLineRanges[2] != "56" {
		t.Fatalf("unexpected split result: line=%q additional=%#v", candidate.LineRange, candidate.AdditionalLineRanges)
	}
	splitIssues := 0
	for _, issue := range issues {
		if issue.Code == IssueLineRangeNormalized && strings.Contains(issue.Message, "space-separated") {
			if !issue.Recoverable {
				t.Fatalf("space split issue must be recoverable: %#v", issue)
			}
			splitIssues++
		}
	}
	if splitIssues < 2 {
		t.Fatalf("expected split issues for line_range and additional entry, got %#v", issues)
	}
}

func TestNormalizeHunterArtifactAcceptsJSONNumberPair(t *testing.T) {
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001",
    "file_path": "src/a.cpp",
    "line_range": "41-44",
    "additional_line_ranges": [70, 91],
    "trigger_line": "x",
    "scope_symbol": "A",
    "category": "测试分类",
    "title": "x",
    "code_snippet": "x",
    "trigger_condition": "x"
  }]
}`
	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), "", nil)
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid normalized JSON: %v\n%s", err, normalized)
	}
	additional := out.Candidates[0].AdditionalLineRanges
	if len(additional) != 1 || additional[0] != "70-91" {
		t.Fatalf("JSON number pair should stay one interval, got %#v", additional)
	}
	for _, issue := range issues {
		if !issue.Recoverable {
			t.Fatalf("JSON number pair must not create unresolved issues, got %#v", issues)
		}
	}
}

func TestNormalizeHunterArtifactEnrichesEmptyTriggerLine(t *testing.T) {
	repoRoot := t.TempDir()
	sourceRel := "src/timer.cpp"
	if err := os.MkdirAll(filepath.Join(repoRoot, filepath.Dir(sourceRel)), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	source := "// line 1\n// line 2\ndelete timer;\n// line 4\n"
	if err := os.WriteFile(filepath.Join(repoRoot, sourceRel), []byte(source), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001",
    "file_path": "` + sourceRel + `",
    "line_range": "3-3",
    "scope_symbol": "A",
    "category": "测试分类",
    "title": "x",
    "code_snippet": "x",
    "trigger_condition": "x"
  }]
}`
	normalized, issues := NormalizeHunterArtifactJSON([]byte(raw), repoRoot, []string{sourceRel})
	var out HunterOutput
	if err := json.Unmarshal(normalized, &out); err != nil {
		t.Fatalf("invalid normalized JSON: %v\n%s", err, normalized)
	}
	if out.Candidates[0].TriggerLine != "delete timer;" {
		t.Fatalf("trigger_line = %q, want enriched source line", out.Candidates[0].TriggerLine)
	}
	enriched := false
	for _, issue := range issues {
		if issue.Code == IssueSnippetEnriched && issue.Field == "trigger_line" {
			enriched = issue.Recoverable
		}
	}
	if !enriched {
		t.Fatalf("expected recoverable trigger_line enrichment issue, got %#v", issues)
	}
}

type artifactRepairInvoker struct {
	raw string
}

func (i *artifactRepairInvoker) Name() string { return "mock-artifact-repair" }
func (i *artifactRepairInvoker) Invoke(req invoker.AIRequest) error {
	return os.WriteFile(req.OutputPath, []byte(i.raw), 0644)
}

func TestParseAndRecoverHunterArtifactSalvagesValidCandidates(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [
    {
      "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
      "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
      "title": "valid", "code_snippet": "x", "trigger_condition": "x"
    },
    {
      "candidate_id": "H-002", "file_path": "src/a.cpp", "line_range": "bad",
      "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
      "title": "invalid", "code_snippet": "x", "trigger_condition": "x"
    }
  ]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: raw})
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:         "mock-artifact-repair",
		MaxSchemaRepairAttempts:      1,
		SchemaRepairTimeoutSeconds:   10,
		MaxQuarantinedCandidateRatio: 0.5,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), raw, contract, repoRoot, bundle, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if recoverErr != nil {
		t.Fatalf("recovery failed: %v", recoverErr)
	}
	if attempts != 1 || successes != 0 {
		t.Fatalf("repair metrics = (%d, %d), want (1, 0)", attempts, successes)
	}
	if state.Quarantined != 1 || state.ArtifactComplete {
		t.Fatalf("quarantine state invalid: quarantined=%d complete=%t", state.Quarantined, state.ArtifactComplete)
	}
	if len(state.Output.Candidates) != 1 || state.Output.Candidates[0].CandidateID != "H-001" {
		t.Fatalf("salvaged candidates = %#v", state.Output.Candidates)
	}
}

func TestParseAndRecoverHunterArtifactKeepsQualityWarningWithoutCandidateLoss(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
    "additional_line_ranges": ["not-a-range"],
    "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
    "title": "valid", "code_snippet": "x", "trigger_condition": "x"
  }]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: raw})
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:         "mock-artifact-repair",
		MaxSchemaRepairAttempts:      1,
		SchemaRepairTimeoutSeconds:   10,
		MaxQuarantinedCandidateRatio: 0.5,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), raw, contract, repoRoot, bundle, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if recoverErr != nil {
		t.Fatalf("recovery failed: %v", recoverErr)
	}
	if attempts != 1 || successes != 0 {
		t.Fatalf("repair metrics = (%d, %d), want (1, 0)", attempts, successes)
	}
	if !state.ArtifactComplete || !state.ArtifactQualityDegraded {
		t.Fatalf("expected complete artifact with quality warning, got complete=%t quality=%t",
			state.ArtifactComplete, state.ArtifactQualityDegraded)
	}
	if state.Quarantined != 0 || state.UnresolvedIssueCount == 0 {
		t.Fatalf("unexpected quarantine/diagnostic state: quarantine=%d unresolved=%d",
			state.Quarantined, state.UnresolvedIssueCount)
	}
	if len(state.Output.Candidates) != 1 {
		t.Fatalf("valid candidate was dropped: %#v", state.Output.Candidates)
	}
}

func TestParseAndRecoverHunterArtifactSuccessfulRepairHasNoResidualIssues(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	brokenRaw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
    "additional_line_ranges": ["not-a-range"],
    "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
    "title": "valid", "code_snippet": "x", "trigger_condition": "x"
  }]
}`
	cleanRaw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
    "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
    "title": "valid", "code_snippet": "x", "trigger_condition": "x"
  }]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: cleanRaw})
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:         "mock-artifact-repair",
		MaxSchemaRepairAttempts:      1,
		SchemaRepairTimeoutSeconds:   10,
		MaxQuarantinedCandidateRatio: 0.5,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), brokenRaw, contract, repoRoot, bundle, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if recoverErr != nil {
		t.Fatalf("recovery failed: %v", recoverErr)
	}
	if attempts != 1 || successes != 1 {
		t.Fatalf("repair metrics = (%d, %d), want (1, 1)", attempts, successes)
	}
	if state.UnresolvedIssueCount != 0 {
		t.Fatalf("unresolved count = %d, want 0 after successful repair", state.UnresolvedIssueCount)
	}
	if messages := unresolvedIssueMessages(state); len(messages) != 0 {
		t.Fatalf("successful repair must not report residual issues, got %#v", messages)
	}
}

func TestParseAndRecoverHunterArtifactRepairsTrailingCommasLocally(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	raw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
    "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
    "title": "valid", "code_snippet": "x", "trigger_condition": "x",
  }]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: raw})
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:       "mock-artifact-repair",
		MaxSchemaRepairAttempts:    1,
		SchemaRepairTimeoutSeconds: 10,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), raw, contract, repoRoot, bundle, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if recoverErr != nil {
		t.Fatalf("syntax recovery failed: %v", recoverErr)
	}
	if attempts != 0 || successes != 0 {
		t.Fatalf("repair metrics = (%d, %d), want zero LLM repair", attempts, successes)
	}
	if state.RepairAudit == nil || state.RepairAudit.SyntaxRepairs != 1 ||
		state.RepairAudit.RepairOutcome != "syntax_normalized" {
		t.Fatalf("syntax audit invalid: %#v", state.RepairAudit)
	}
	if !state.ArtifactComplete || len(state.Output.Candidates) != 1 {
		t.Fatalf("syntax-normalized artifact invalid: complete=%t candidates=%#v",
			state.ArtifactComplete, state.Output)
	}
}

func TestParseAndRecoverHunterArtifactAcceptsUnverifiedRepairAsQualityDegraded(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	if err != nil {
		t.Fatalf("contract: %v", err)
	}

	repairRaw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [{
    "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
    "trigger_line": "x", "scope_symbol": "A", "category": "测试分类",
    "title": "valid", "code_snippet": "x", "trigger_condition": "x"
  }]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: repairRaw})
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:       "mock-artifact-repair",
		MaxSchemaRepairAttempts:    1,
		SchemaRepairTimeoutSeconds: 10,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), `{}`, contract, repoRoot, bundle, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if recoverErr != nil {
		t.Fatalf("unverified repair failed: %v", recoverErr)
	}
	if attempts != 1 || successes != 1 {
		t.Fatalf("repair metrics = (%d, %d), want (1, 1)", attempts, successes)
	}
	if !state.ArtifactQualityDegraded || !state.RepairAudit.UnverifiedRepair ||
		state.RepairAudit.RepairOutcome != "unverified_quality_degraded" {
		t.Fatalf("unverified repair state invalid: state=%#v audit=%#v", state, state.RepairAudit)
	}
	found := false
	for _, issue := range state.Issues {
		if issue.Code == IssueRepairBaselineUnverified && issue.Recoverable {
			found = true
		}
	}
	if !found {
		t.Fatalf("baseline-unverified issue missing: %#v", state.Issues)
	}
}

func TestParseAndRecoverHunterArtifactRepairsSchemaThenCategories(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "bundle", AllFiles: []string{"src/a.cpp"}}
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatal(err)
	}

	brokenRaw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [
    {
      "candidate_id": "H-001", "file_path": "/src/a.cpp", "line_range": "1-2",
      "trigger_line": "x", "scope_symbol": "A", "category_code": "RACE_RELEASE_ACCESS",
      "category": "时序与初始化问题-释放与访问竞态", "title": "anchor", "code_snippet": "x", "trigger_condition": "x"
    },
    {
      "candidate_id": "H-002", "file_path": "src/a.cpp", "line_range": "3-4",
      "trigger_line": "x", "scope_symbol": "A", "category_code": "",
      "category": "unknown", "title": "category", "code_snippet": "x", "trigger_condition": "x"
    }
  ]
}`
	repairedRaw := `{
  "schema": "` + CandidatesArtifactSchemaV1 + `",
  "candidates": [
    {
      "candidate_id": "H-001", "file_path": "src/a.cpp", "line_range": "1-2",
      "trigger_line": "x", "scope_symbol": "A", "category_code": "RACE_RELEASE_ACCESS",
      "category": "时序与初始化问题-释放与访问竞态", "title": "anchor", "code_snippet": "x", "trigger_condition": "x"
    },
    {
      "candidate_id": "H-002", "file_path": "src/a.cpp", "line_range": "3-4",
      "trigger_line": "x", "scope_symbol": "A", "category_code": "",
      "category": "unknown", "title": "category", "code_snippet": "x", "trigger_condition": "x"
    }
  ]
}`
	invoker.RegisterAIInvoker("mock-artifact-repair", &artifactRepairInvoker{raw: repairedRaw})
	categoryScript := registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-002","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync","needs_review":false}]}`)
	previous := models.AppConfig.Scanner.Artifact
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:         "mock-artifact-repair",
		MaxSchemaRepairAttempts:      1,
		SchemaRepairTimeoutSeconds:   10,
		CategoryRepairEnabled:        &[]bool{true}[0],
		CategoryRepairResource:       "native",
		CategoryRepairTimeoutSeconds: 60,
		CategoryRepairMaxCandidates:  20,
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	state, attempts, successes, _, recoverErr := parseAndRecoverHunterArtifact(
		context.Background(), brokenRaw, contract, repoRoot, bundle, nil, testTaxonomy(),
	)
	if recoverErr != nil {
		t.Fatalf("recovery failed: %v", recoverErr)
	}
	if attempts != 2 || successes != 2 {
		t.Fatalf("repair metrics = (%d, %d), want schema + category (2, 2)", attempts, successes)
	}
	if categoryScript.timeoutSeconds == 0 {
		t.Fatal("category repair was skipped after schema repair")
	}
	if len(state.Output.Candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(state.Output.Candidates))
	}
	if state.Output.Candidates[0].FilePath != "src/a.cpp" {
		t.Fatalf("anchor repair failed: %#v", state.Output.Candidates[0])
	}
	if state.Output.Candidates[1].CategoryCode != "RACE_RELEASE_ACCESS" || state.Output.Candidates[1].ReviewRequired {
		t.Fatalf("category repair failed: %#v", state.Output.Candidates[1])
	}
}

func TestBuildArtifactRepairPromptContainsIssuesAndContract(t *testing.T) {
	contract, _ := ContractForStage("debate_full", "hunter", []string{"测试分类"})
	prompt := buildArtifactRepairPrompt(`{"schema":"x"}`, contract, []ArtifactIssue{{
		Stage: "hunter", Schema: contract.SchemaID, Code: IssueUnresolved,
		Field: "line_range", Message: "invalid line range",
	}})
	for _, want := range []string{
		"Service-side Validation Issues",
		"invalid line range",
		CandidatesArtifactSchemaV1,
		"additional_line_ranges",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, prompt)
		}
	}
}
