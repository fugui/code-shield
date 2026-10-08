package debate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
)

type hunterAuditInvoker struct {
	initial      string
	repair       string
	repairErr    error
	invocations  int
	promptBodies []string
}

type hunterMetricsRepairInvoker struct {
	raw     string
	err     error
	name    string
	attempt int
}

func (inv *hunterMetricsRepairInvoker) Name() string { return inv.name }

func (inv *hunterMetricsRepairInvoker) Invoke(request invoker.AIRequest) error {
	inv.attempt++
	if inv.err != nil {
		return inv.err
	}
	return os.WriteFile(request.OutputPath, []byte(inv.raw), 0640)
}

func (inv *hunterAuditInvoker) Name() string { return "hunter-audit-invoker" }

func (inv *hunterAuditInvoker) Invoke(request invoker.AIRequest) error {
	inv.invocations++
	inv.promptBodies = append(inv.promptBodies, request.PromptMsg)
	if inv.invocations > 1 {
		if inv.repairErr != nil {
			return inv.repairErr
		}
		if writeErr := os.WriteFile(request.OutputPath, []byte(inv.repair), 0640); writeErr != nil {
			return writeErr
		}
		return nil
	}
	return os.WriteFile(request.OutputPath, []byte(inv.initial), 0640)
}

func configureHunterAudit(t *testing.T, inv *hunterAuditInvoker) {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, "shield_hunter_repair_audit", &models.ArtifactRepairAudit{})
	models.DB = db
	t.Cleanup(func() { models.DB = nil })
	invoker.RegisterAIInvoker(inv.Name(), inv)

	previousArtifact := models.AppConfig.Scanner.Artifact
	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	recovery := &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}}
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:       inv.Name(),
		MaxSchemaRepairAttempts:    1,
		SchemaRepairTimeoutSeconds: 10,
	}
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource:              inv.Name(),
		Resources:             []string{inv.Name()},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		MaxSplitDepth:         1,
		Recovery:              recovery,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Artifact = previousArtifact
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = previousTier
	})

}

func runHunterAuditStage(t *testing.T, bundleName string, outName string, allowError bool) error {
	t.Helper()
	tempDir := t.TempDir()
	files := []string{"src/a.cpp"}
	engineCtx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		ReportID:   42,
		RepoID:     7,
		TaskTypeID: 9,
		CodesPath:  tempDir,
		EngineMode: "debate_full",
	}
	bundle := &chunker.SemanticBundle{Name: bundleName, PrimaryFiles: files, AllFiles: files}
	_, _, err := (&DebateEngine{}).runHunterStageOnceWithPlan(
		engineCtx,
		*bundle,
		filepath.Join(tempDir, outName),
		10,
		dispatcher.TierCandidate{ResourceID: "resource-1", Driver: "hunter-audit-invoker", Model: "audit-model"},
	)
	if err != nil {
		t.Fatalf("runHunterStageOnceWithPlan() error = %v", err)
	}
	return nil
}

func lastHunterAudit(t *testing.T) models.ArtifactRepairAudit {
	t.Helper()
	var audit models.ArtifactRepairAudit
	if models.DB == nil {
		t.Fatal("models.DB is nil")
	}
	if err := models.DB.Order("id DESC").First(&audit).Error; err != nil {
		t.Fatalf("query hunter audit: %v", err)
	}
	return audit
}

func decodeHunterAttempts(t *testing.T, audit models.ArtifactRepairAudit) []HunterArtifactRepairAttempt {
	t.Helper()
	var attempts []HunterArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode hunter attempts: %v", err)
	}
	return attempts
}

func TestHunterArtifactRepairAuditValidDirectPass(t *testing.T) {
	inv := &hunterAuditInvoker{initial: `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[]}`}
	configureHunterAudit(t, inv)
	_ = runHunterAuditStage(t, "audit-valid", "hunter.json", false)
	audit := lastHunterAudit(t)
	if audit.SchemaID != CandidatesArtifactSchemaV1 || audit.SchemaHash == "" ||
		audit.FinalStatus != "success" || audit.LLMRepairAttempts != 0 || audit.LocalRepairs != 0 {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestSanitizePostgreSQLJSONRemovesNul(t *testing.T) {
	attempts := []ArtifactRepairAttempt{{
		Attempt: 1,
		Raw:     "broken\x00 output",
	}}

	encoded, err := json.Marshal(attempts)
	if err != nil {
		t.Fatalf("marshal attempts: %v", err)
	}
	sanitized := sanitizePostgreSQLJSON(encoded)
	if strings.Contains(string(sanitized), "\x00") {
		t.Fatalf("sanitized JSON still contains NUL: %q", string(sanitized))
	}
	var decoded []ArtifactRepairAttempt
	if err := json.Unmarshal(sanitized, &decoded); err != nil {
		t.Fatalf("decode sanitized attempts: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Raw != "broken output" {
		t.Fatalf("unexpected sanitized attempts: %+v", decoded)
	}
}

func TestHunterRepairAuditTracksLLMRepairMetrics(t *testing.T) {
	inv := &hunterMetricsRepairInvoker{
		name: "hunter-metrics-repair-success",
		raw:  `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"H-001","file_path":"src/a.cpp","line_range":"1-2","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"valid","code_snippet":"x","trigger_condition":"x"}]}`,
	}
	invoker.RegisterAIInvoker(inv.Name(), inv)
	previousArtifact := models.AppConfig.Scanner.Artifact
	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	recovery := &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}}
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:         inv.Name(),
		MaxSchemaRepairAttempts:      1,
		MaxQuarantinedCandidateRatio: 0.5,
	}
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource: inv.Name(), Resources: []string{inv.Name()}, Recovery: recovery,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Artifact = previousArtifact
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = previousTier
	})

	state, _, _, _, err := parseAndRecoverHunterArtifact(
		context.Background(), `{}`, mustHunterContract(t), "", chunker.SemanticBundle{}, nil, models.CategoryTaxonomy{},
	)
	if err != nil {
		t.Fatalf("parseAndRecoverHunterArtifact() error = %v", err)
	}
	if state.RepairAudit == nil || state.RepairAudit.LLMRepairAttempts != 1 ||
		state.RepairAudit.LLMRepairSuccesses != 1 || len(state.RepairAudit.RepairAttempts) != 1 ||
		state.RepairAudit.RepairAttempts[0].Raw == "" {
		t.Fatalf("repair audit = %#v", state.RepairAudit)
	}
}

func TestHunterRepairAuditTracksDriftedRepair(t *testing.T) {
	initial := `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"H-001","file_path":"src/a.cpp","line_range":"bad","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"valid","code_snippet":"x","trigger_condition":"x"}]}`
	inv := &hunterMetricsRepairInvoker{
		name: "hunter-metrics-repair-drift",
		raw:  strings.Replace(strings.Replace(initial, "src/a.cpp", "src/changed.cpp", 1), `"line_range":"bad"`, `"line_range":"1-2"`, 1),
	}
	invoker.RegisterAIInvoker(inv.Name(), inv)
	previousArtifact := models.AppConfig.Scanner.Artifact
	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	recovery := &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}}
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:    inv.Name(),
		MaxSchemaRepairAttempts: 1,
	}
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource: inv.Name(), Resources: []string{inv.Name()}, Recovery: recovery,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Artifact = previousArtifact
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = previousTier
	})

	state, _, _, _, err := parseAndRecoverHunterArtifact(
		context.Background(), initial, mustHunterContract(t), "", chunker.SemanticBundle{}, []string{"测试分类"}, models.CategoryTaxonomy{},
	)
	if err == nil {
		t.Fatal("parseAndRecoverHunterArtifact() should reject drift")
	}
	if state.RepairAudit == nil || !state.RepairAudit.RepairDrifted || state.RepairAudit.RepairDriftUnitRef != "H-001" || state.RepairAudit.FinalStatus != "failed" ||
		len(state.RepairAudit.RepairAttempts) != 2 || state.RepairAudit.RepairAttempts[1].Accepted ||
		state.RepairAudit.RepairAttempts[1].Raw == "" {
		t.Fatalf("repair audit = %#v", state.RepairAudit)
	}
}

func TestHunterRepairAuditValidArtifactHasSchemaMetrics(t *testing.T) {
	raw := `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[]}`
	state, _, _, _, err := parseAndRecoverHunterArtifact(
		context.Background(), raw, mustHunterContract(t), "", chunker.SemanticBundle{}, nil, models.CategoryTaxonomy{},
	)
	if err != nil {
		t.Fatalf("parseAndRecoverHunterArtifact() error = %v", err)
	}
	audit := state.RepairAudit
	if audit == nil || audit.SchemaID != CandidatesArtifactSchemaV1 || audit.SchemaHash == "" ||
		audit.OriginalArtifactHash == "" || audit.FinalStatus != "success" ||
		audit.LocalRepairs != 0 || audit.LLMRepairAttempts != 0 || len(audit.RepairAttempts) != 0 {
		t.Fatalf("repair audit = %#v", audit)
	}
}

func TestHunterRepairAuditTracksFailedLLMRepair(t *testing.T) {
	inv := &hunterMetricsRepairInvoker{
		name: "hunter-metrics-repair-failure",
		err:  errors.New("repair unavailable"),
	}
	invoker.RegisterAIInvoker(inv.Name(), inv)
	previousArtifact := models.AppConfig.Scanner.Artifact
	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	recovery := &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}}
	models.AppConfig.Scanner.Artifact = models.ArtifactGuardConfig{
		SchemaRepairResource:    inv.Name(),
		MaxSchemaRepairAttempts: 1,
	}
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource: inv.Name(), Resources: []string{inv.Name()}, Recovery: recovery,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Artifact = previousArtifact
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = previousTier
	})

	state, _, _, _, err := parseAndRecoverHunterArtifact(
		context.Background(), `{}`, mustHunterContract(t), "", chunker.SemanticBundle{}, nil, models.CategoryTaxonomy{},
	)
	if err == nil {
		t.Fatal("parseAndRecoverHunterArtifact() should fail")
	}
	if state.RepairAudit == nil || state.RepairAudit.FinalStatus != "failed" ||
		state.RepairAudit.LLMRepairAttempts != 1 || state.RepairAudit.LLMRepairSuccesses != 0 ||
		len(state.RepairAudit.RepairAttempts) != 1 || state.RepairAudit.RepairAttempts[0].Accepted ||
		state.RepairAudit.RepairAttempts[0].Raw != "" || state.RepairAudit.RepairAttempts[0].Error == "" {
		t.Fatalf("repair audit = %#v", state.RepairAudit)
	}
}

func TestHunterArtifactRepairAuditLocalRepair(t *testing.T) {
	initial := "```json\n{\"schema\":\"" + CandidatesArtifactSchemaV1 + "\",\"candidates\":[]}\n```"
	inv := &hunterAuditInvoker{initial: initial}
	configureHunterAudit(t, inv)
	_ = runHunterAuditStage(t, "audit-local", "hunter.json", false)
	audit := lastHunterAudit(t)
	attempts := decodeHunterAttempts(t, audit)
	if audit.LocalRepairs != 1 || audit.FinalStatus != "success" || len(attempts) != 1 ||
		!attempts[0].Accepted || attempts[0].Source != "local" || attempts[0].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestHunterArtifactRepairAuditLLMRepairSuccess(t *testing.T) {
	inv := &hunterAuditInvoker{
		initial: `{}`,
		repair:  `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[]}`,
	}
	configureHunterAudit(t, inv)
	_ = runHunterAuditStage(t, "audit-llm", "hunter.json", false)
	audit := lastHunterAudit(t)
	attempts := decodeHunterAttempts(t, audit)
	if audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 1 || audit.FinalStatus != "success" ||
		len(attempts) != 1 || !attempts[0].Accepted || attempts[0].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestHunterArtifactRepairAuditLLMFailure(t *testing.T) {
	inv := &hunterAuditInvoker{initial: `{}`, repairErr: errors.New("repair unavailable")}
	configureHunterAudit(t, inv)
	_ = runHunterAuditStage(t, "audit-failure", "hunter.json", true)
	audit := lastHunterAudit(t)
	attempts := decodeHunterAttempts(t, audit)
	if audit.FinalStatus != "failed" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 0 ||
		len(attempts) != 1 || attempts[0].Accepted || attempts[0].Raw != "" || attempts[0].Error == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestHunterArtifactRepairAuditDriftRejected(t *testing.T) {
	inv := &hunterAuditInvoker{
		initial: `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"H-001","file_path":"src/a.cpp","line_range":"1-2","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"valid","code_snippet":"x","trigger_condition":"x"}]}`,
		repair:  `{"schema":"` + CandidatesArtifactSchemaV1 + `","candidates":[{"candidate_id":"H-001","file_path":"src/changed.cpp","line_range":"1-2","trigger_line":"x","scope_symbol":"A","category":"测试分类","title":"valid","code_snippet":"x","trigger_condition":"x"}]}`,
	}
	configureHunterAudit(t, inv)
	_ = runHunterAuditStage(t, "audit-drift", "hunter.json", true)
	audit := lastHunterAudit(t)
	attempts := decodeHunterAttempts(t, audit)
	if !audit.RepairDrifted || audit.FinalStatus != "failed" || len(attempts) != 1 ||
		attempts[0].Accepted || attempts[0].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}
