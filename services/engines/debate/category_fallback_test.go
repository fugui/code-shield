package debate

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

func rawHunterCandidate(code, label string) string {
	value := map[string]any{
		"schema": "code-shield.candidates.v1",
		"candidates": []map[string]any{{
			"candidate_id": "H-010", "file_path": "src/a.cpp", "line_range": "42-50",
			"trigger_line": "unlock(); access()", "scope_symbol": "A::f",
			"category_code": code, "category": label,
			"title": "race", "code_snippet": "x", "trigger_condition": "x",
		}},
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func disableCategoryRepair(t *testing.T) {
	t.Helper()
	previous := models.AppConfig.Scanner.Artifact
	disabled := false
	previous.CategoryRepairEnabled = &disabled
	models.AppConfig.Scanner.Artifact = previous
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })
}

func TestHunterCategoryOnlyFailurePreservesCandidate(t *testing.T) {
	disableCategoryRepair(t)
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := parseHunterArtifactState(rawHunterCandidate("", "生命周期-释放后使用"), contract, "", chunker.SemanticBundle{}, nil, testTaxonomy())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.ValidCandidates) != 1 || len(state.InvalidCandidates) != 0 || state.Quarantined != 0 {
		t.Fatalf("candidate was quarantined: valid=%d invalid=%d quarantined=%d", len(state.ValidCandidates), len(state.InvalidCandidates), state.Quarantined)
	}
	if !state.ArtifactComplete || !state.ArtifactQualityDegraded {
		t.Fatalf("fallback flags invalid: complete=%t degraded=%t", state.ArtifactComplete, state.ArtifactQualityDegraded)
	}
	candidate := state.ValidCandidates[0]
	if candidate.CategoryCode != "" || candidate.Category != "未分类" || !candidate.ReviewRequired {
		t.Fatalf("review candidate invalid: %#v", candidate)
	}
}

func TestHunterDeterministicCategoryRepair(t *testing.T) {
	disableCategoryRepair(t)
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := parseHunterArtifactState(rawHunterCandidate("race_release_access", "wrong"), contract, "", chunker.SemanticBundle{}, nil, testTaxonomy())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.ValidCandidates) != 1 {
		t.Fatalf("valid candidates = %d", len(state.ValidCandidates))
	}
	candidate := state.ValidCandidates[0]
	if candidate.CategoryCode != "RACE_RELEASE_ACCESS" || candidate.Category != testTaxonomy().Categories[0].Label || candidate.ReviewRequired {
		t.Fatalf("deterministic repair failed: %#v", candidate)
	}
}

func TestHunterAnchorFailureStillQuarantines(t *testing.T) {
	disableCategoryRepair(t)
	contract, _ := ContractForStage("debate_full", "hunter", nil)
	anchorBroken := strings.Replace(strings.Replace(rawHunterCandidate("RACE_RELEASE_ACCESS", ""), `"file_path":"src/a.cpp"`, `"file_path":"/src/a.cpp"`, 1), `"file_path": "src/a.cpp"`, `"file_path": "/src/a.cpp"`, 1)
	state, _ := parseHunterArtifactState(anchorBroken, contract, "", chunker.SemanticBundle{}, nil, testTaxonomy())
	if state == nil || len(state.InvalidCandidates) != 1 || state.Quarantined != 0 {
		t.Fatalf("anchor candidate should enter invalid path before salvage: %#v", state)
	}
}

func TestParseCategoryRepairOutputValidates(t *testing.T) {
	requests := []CategoryRepairRequest{{CandidateID: "H-010"}}
	valid := `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync"}]}`
	if repairs, _, err := parseCategoryRepairOutput(valid, requests, testTaxonomy()); err != nil || len(repairs) != 1 {
		t.Fatalf("valid repair rejected: repairs=%v err=%v", repairs, err)
	}
	if _, _, err := parseCategoryRepairOutput(valid, nil, testTaxonomy()); err == nil {
		t.Fatal("unexpected candidate id accepted")
	}
	if _, _, err := parseCategoryRepairOutput(`{"repairs":[]}`, requests, testTaxonomy()); err == nil {
		t.Fatal("missing candidate id accepted")
	}
	badCode := `{"repairs":[{"candidate_id":"H-010","category_code":"RACE","classification_rationale":"sync"}]}`
	if _, _, err := parseCategoryRepairOutput(badCode, requests, testTaxonomy()); err == nil {
		t.Fatal("invalid code accepted")
	}
	noRationale := `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":""}]}`
	if _, _, err := parseCategoryRepairOutput(noRationale, requests, testTaxonomy()); err == nil {
		t.Fatal("empty rationale accepted")
	}
	emptyRationaleWithoutCode := `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":""}]}`
	if _, _, err := parseCategoryRepairOutput(emptyRationaleWithoutCode, requests, testTaxonomy()); err == nil {
		t.Fatal("empty rationale accepted")
	}
	illegalPatchField := `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync","title":"changed"}]}`
	repairs, auditIssues, err := parseCategoryRepairOutput(illegalPatchField, requests, testTaxonomy())
	if err != nil {
		t.Fatalf("ignored extra field rejected repair: %v", err)
	}
	if len(repairs) != 1 || len(auditIssues) != 1 {
		t.Fatalf("extra-field audit invalid: repairs=%d issues=%d", len(repairs), len(auditIssues))
	}
	if auditIssues[0].Code != IssueCategoryExtraFieldIgnored || auditIssues[0].Field != "title" {
		t.Fatalf("extra-field issue invalid: %#v", auditIssues[0])
	}
	if auditIssues[0].RawArtifact != illegalPatchField {
		t.Fatalf("raw repair artifact not retained: %#v", auditIssues[0])
	}
}

func TestCategoryRepairMaxCandidatesFailsBeforeInvocation(t *testing.T) {
	previous := models.AppConfig.Scanner.Artifact
	max := 1
	previous.CategoryRepairMaxCandidates = max
	enabled := true
	previous.CategoryRepairEnabled = &enabled
	models.AppConfig.Scanner.Artifact = previous
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })

	requests := []CategoryRepairRequest{{CandidateID: "H-001"}, {CandidateID: "H-002"}}
	if _, _, _, err := executeCategoryRepairs(context.Background(), "", requests, testTaxonomy(), OutputContract{}, "test"); err == nil ||
		!strings.Contains(err.Error(), "exceeds limit 1") {
		t.Fatalf("expected max candidates error, got %v", err)
	}
}

func TestJudgeCategoryOnlyFailureDoesNotRejectOutput(t *testing.T) {
	disableCategoryRepair(t)
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{{
		CandidateID: "H-010", Verdict: "CONFIRMED", SeverityPreliminary: "严重",
		Category: "释放访问", FilePath: "src/a.cpp", LineRange: "42-50",
		TriggerLine: "x", ScopeSymbol: "A", Title: "x", JudgementRationale: "x",
		CodeSnippet: "x", Suggestion: "x", Evidence: []JudgeEvidenceRef{{EvidenceID: "e", Kind: JudgeEvidenceCaller, Path: "src/a.cpp", LineRange: "42-50", Snippet: "x", Reason: "x"}},
	}}}
	if err := validateJudgeOutput(out, []string{"H-010"}, nil, testTaxonomy()); err != nil {
		t.Fatalf("category-only failure rejected judge output: %v", err)
	}
	if !out.FinalVerdicts[0].ReviewRequired || out.FinalVerdicts[0].CategoryCode != "" {
		t.Fatalf("judge review state invalid: %#v", out.FinalVerdicts[0])
	}
	if _, _, _, _, err := parseAndRecoverHunterArtifact(context.Background(), rawHunterCandidate("", "wrong"), mustHunterContract(t), "", chunker.SemanticBundle{}, nil, testTaxonomy()); err != nil {
		t.Fatalf("review fallback returned error: %v", err)
	}
}

type categoryRepairScript struct {
	response       string
	timeoutSeconds int
	temperature    float64
}

func (script *categoryRepairScript) Invoke(request invoker.AIRequest) error {
	script.timeoutSeconds = request.TimeoutSeconds
	if request.Temperature != nil {
		script.temperature = *request.Temperature
	}
	return os.WriteFile(request.OutputPath, []byte(script.response), 0644)
}

func (script *categoryRepairScript) Name() string { return "category-repair-script" }

func registerCategoryRepairScript(t *testing.T, response string) *categoryRepairScript {
	t.Helper()
	script := &categoryRepairScript{response: response}
	invoker.RegisterAIInvoker("native", script)
	previous := models.AppConfig.Scanner.Artifact
	enabled := true
	previous.CategoryRepairResource = "native"
	previous.CategoryRepairEnabled = &enabled
	models.AppConfig.Scanner.Artifact = previous
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact = previous })
	return script
}

func judgeReviewVerdict(categoryCode, category string) JudgeFinalVerdict {
	return JudgeFinalVerdict{
		CandidateID: "H-010", Verdict: "CONFIRMED", SeverityPreliminary: "严重",
		CategoryCode: categoryCode, Category: category, FilePath: "src/a.cpp", LineRange: "42-50",
		TriggerLine: "x", ScopeSymbol: "A", Title: "x", JudgementRationale: "x",
		CodeSnippet: "x", Suggestion: "x", Evidence: []JudgeEvidenceRef{{
			EvidenceID: "e", Kind: JudgeEvidenceCaller, Path: "src/a.cpp", LineRange: "42-50", Snippet: "x", Reason: "x",
		}},
	}
}

func TestJudgeCategoryRepairSuccess(t *testing.T) {
	script := registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync","needs_review":false}]}`)
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{judgeReviewVerdict("BAD_CODE", "invalid")}}
	if err := validateJudgeOutput(out, []string{"H-010"}, nil, testTaxonomy()); err != nil {
		t.Fatal(err)
	}
	tokens, attempts, err := repairJudgeCategories(
		context.Background(), "", out, []HunterCandidate{{CandidateID: "H-010", CategoryCode: "OLD", Category: "hunter"}}, testTaxonomy(), OutputContract{},
	)
	if err != nil || tokens <= 0 || attempts != 1 {
		t.Fatalf("repair result: tokens=%d attempts=%d err=%v", tokens, attempts, err)
	}
	verdict := out.FinalVerdicts[0]
	if verdict.CategoryCode != "RACE_RELEASE_ACCESS" || verdict.Category != testTaxonomy().Categories[0].Label || verdict.ReviewRequired {
		t.Fatalf("judge repair failed: %#v", verdict)
	}
	if verdict.ClassificationRationale != "sync" {
		t.Fatalf("classification rationale = %q", verdict.ClassificationRationale)
	}
	if out.CategoryMetrics.LLMRepairAttempts != 1 || out.CategoryMetrics.LLMRepairs != 1 || out.CategoryMetrics.ReviewRequired != 0 {
		t.Fatalf("judge repair metrics invalid: %#v", out.CategoryMetrics)
	}
	if script.timeoutSeconds != 60 || script.temperature != 0 {
		t.Fatalf("repair invocation invalid: timeout=%d temperature=%f", script.timeoutSeconds, script.temperature)
	}
}

func TestJudgeRejectedCandidatesDoNotEnterCategoryRepair(t *testing.T) {
	script := registerCategoryRepairScript(t, `{"repairs":[]}`)
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{{
		CandidateID: "H-010", Verdict: "REJECTED", SeverityPreliminary: "严重",
		Category: "invalid", ReviewRequired: true, FilePath: "src/a.cpp", LineRange: "42-50",
		TriggerLine: "x", ScopeSymbol: "A", Title: "x", JudgementRationale: "x",
		CodeSnippet: "x", Suggestion: "x",
	}}}
	tokens, attempts, err := repairJudgeCategories(context.Background(), "", out, nil, testTaxonomy(), OutputContract{})
	if err != nil || tokens != 0 || attempts != 0 {
		t.Fatalf("rejected repair attempted: tokens=%d attempts=%d err=%v", tokens, attempts, err)
	}
	if script.timeoutSeconds != 0 {
		t.Fatalf("rejected candidate invoked repair backend: %#v", script)
	}
	verdict := out.FinalVerdicts[0]
	if verdict.Category != "invalid" || verdict.CategoryCode != "" || !verdict.ReviewRequired {
		t.Fatalf("rejected verdict was mutated: %#v", verdict)
	}
	if out.CategoryMetrics.LLMRepairAttempts != 0 || out.CategoryMetrics.LLMRepairs != 0 {
		t.Fatalf("rejected repair metrics invalid: %#v", out.CategoryMetrics)
	}
}

func TestJudgeRepairFailureMetricsAreExact(t *testing.T) {
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{judgeReviewVerdict("BAD_CODE", "invalid")}}
	registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-010","category_code":"BAD_CODE","classification_rationale":"sync"}]}`)
	if err := validateJudgeOutput(out, []string{"H-010"}, nil, testTaxonomy()); err != nil {
		t.Fatal(err)
	}
	_, _, err := repairJudgeCategories(context.Background(), "", out, nil, testTaxonomy(), OutputContract{})
	if err == nil {
		t.Fatal("expected repair failure")
	}
	if out.CategoryMetrics.LLMRepairAttempts != 1 || out.CategoryMetrics.LLMRepairs != 0 {
		t.Fatalf("repair metrics = %d/%d, want 1/0", out.CategoryMetrics.LLMRepairAttempts, out.CategoryMetrics.LLMRepairs)
	}
}

func TestHunterCategoryRepairAuditsExtraFields(t *testing.T) {
	registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync","title":"changed business field"}]}`)
	state, err := parseHunterArtifactState(rawHunterCandidate("BAD", "invalid"), mustHunterContract(t), "", chunker.SemanticBundle{}, nil, testTaxonomy())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, repairErr := repairHunterCategories(context.Background(), "", state, mustHunterContract(t)); repairErr != nil {
		t.Fatal(repairErr)
	}
	candidate := state.Output.Candidates[0]
	if candidate.Title != "race" || candidate.CategoryCode != "RACE_RELEASE_ACCESS" {
		t.Fatalf("category repair changed forbidden fields: %#v", candidate)
	}
	var auditIssue *ArtifactIssue
	for i := range state.Issues {
		if state.Issues[i].Code == IssueCategoryExtraFieldIgnored {
			auditIssue = &state.Issues[i]
			break
		}
	}
	if auditIssue == nil || auditIssue.Field != "title" || auditIssue.CandidateID != "H-010" {
		t.Fatalf("extra field audit issue missing: %#v", state.Issues)
	}
	if !strings.Contains(auditIssue.RawArtifact, `"title":"changed business field"`) {
		t.Fatalf("raw repair artifact untraceable: %q", auditIssue.RawArtifact)
	}
	auditIssueBytes, err := json.Marshal(state.RepairAudit.SchemaRepairIssues)
	if err != nil {
		t.Fatalf("marshal audit issues: %v", err)
	}
	if strings.Contains(string(auditIssueBytes), `"title":"changed business field"`) {
		t.Fatalf("raw repair artifact leaked into persisted issues: %s", auditIssueBytes)
	}
}

func TestJudgeCategoryRepairFailureInheritsHunter(t *testing.T) {
	registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-010","category_code":"BAD_CODE","classification_rationale":"sync"}]}`)
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{judgeReviewVerdict("BAD_CODE", "invalid")}}
	if err := validateJudgeOutput(out, []string{"H-010"}, nil, testTaxonomy()); err != nil {
		t.Fatal(err)
	}
	hunter := HunterCandidate{CandidateID: "H-010", CategoryCode: "HUNTER_CODE", Category: "hunter label"}
	if _, _, err := repairJudgeCategories(context.Background(), "", out, []HunterCandidate{hunter}, testTaxonomy(), OutputContract{}); err == nil {
		t.Fatal("invalid repair accepted")
	}
	if out.CategoryMetrics.LLMRepairAttempts != 1 || out.CategoryMetrics.LLMRepairs != 0 {
		t.Fatalf("failure metrics invalid: %#v", out.CategoryMetrics)
	}
	code, source, status := resolveJudgeCategoryInheritance(&out.FinalVerdicts[0], hunter)
	if code != "HUNTER_CODE" || source != "judge_inherit" || status != "REVIEW_REQUIRED" {
		t.Fatalf("inheritance invalid: code=%q source=%q status=%q", code, source, status)
	}
	if out.FinalVerdicts[0].Category != "hunter label" {
		t.Fatalf("hunter label not inherited: %#v", out.FinalVerdicts[0])
	}
}

func TestJudgeCategoryRepairDisabledFallback(t *testing.T) {
	disableCategoryRepair(t)
	out := &JudgeOutput{FinalVerdicts: []JudgeFinalVerdict{judgeReviewVerdict("BAD_CODE", "invalid")}}
	if err := validateJudgeOutput(out, []string{"H-010"}, nil, testTaxonomy()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repairJudgeCategories(
		context.Background(), "", out, nil, testTaxonomy(), OutputContract{},
	); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if !out.FinalVerdicts[0].ReviewRequired {
		t.Fatal("disabled repair must preserve review fallback")
	}
}

func TestCategoryRepairTimeoutIsExplicit(t *testing.T) {
	script := registerCategoryRepairScript(t, `{"repairs":[{"candidate_id":"H-010","category_code":"RACE_RELEASE_ACCESS","classification_rationale":"sync"}]}`)
	previous := models.AppConfig.Scanner.Artifact
	previous.CategoryRepairTimeoutSeconds = 121
	models.AppConfig.Scanner.Artifact = previous
	state, err := parseHunterArtifactState(rawHunterCandidate("BAD", "invalid"), mustHunterContract(t), "", chunker.SemanticBundle{}, nil, testTaxonomy())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, repairErr := repairHunterCategories(context.Background(), "", state, mustHunterContract(t)); repairErr != nil {
		t.Fatal(repairErr)
	}
	if script.timeoutSeconds != 121 {
		t.Fatalf("timeout seconds = %d, want 121", script.timeoutSeconds)
	}
}

func mustHunterContract(t *testing.T) OutputContract {
	t.Helper()
	contract, err := ContractForStage("debate_full", "hunter", nil)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}
