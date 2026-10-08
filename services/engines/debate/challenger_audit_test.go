package debate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type challengerAuditInvoker struct {
	name         string
	initial      string
	repair       string
	repairEr     error
	outputByCall map[int]string
	calls        int
	prompts      []string
}

func (inv *challengerAuditInvoker) Name() string { return inv.name }

func (inv *challengerAuditInvoker) Invoke(request invoker.AIRequest) error {
	inv.calls++
	inv.prompts = append(inv.prompts, request.PromptMsg)
	payload := ""
	if inv.calls == 1 {
		payload = inv.initial
	} else {
		if inv.repairEr != nil {
			return inv.repairEr
		}
		payload = inv.repair
	}
	if byCall, ok := inv.outputByCall[inv.calls]; ok {
		payload = byCall
	}
	return os.WriteFile(request.OutputPath, []byte(payload), 0640)
}

func setupChallengerAudit(t *testing.T, auditInvoker *challengerAuditInvoker) *gorm.DB {
	t.Helper()
	tempDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(tempDir, "audit.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.ArtifactRepairAudit{}); err != nil {
		t.Fatalf("migrate audit: %v", err)
	}
	models.DB = db
	t.Cleanup(func() { models.DB = nil })
	invoker.RegisterAIInvoker(auditInvoker.Name(), auditInvoker)

	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier2Challenger
	models.AppConfig.Scanner.Debate.Tiers.Tier2Challenger = models.TierBindingConfig{
		Resource:              auditInvoker.Name(),
		Resources:             []string{auditInvoker.Name()},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		MaxSplitDepth:         1,
		Recovery:              &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}},
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Debate.Tiers.Tier2Challenger = previousTier })
	return db
}

func runChallengerAudit(t *testing.T, candidates []HunterCandidate) (*ChallengerOutput, *gorm.DB) {
	t.Helper()
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "a.cpp"), []byte("if (!value) return;\nreturn value;\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	engineCtx := &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		ReportID:   42,
		RepoID:     7,
		TaskTypeID: 9,
		CodesPath:  tempDir,
		EngineMode: "debate_full",
		DefenseDimensions: []models.DefenseDimension{
			{Key: "Guards", Name: "前置防御事实", Description: "outer guard evidence"},
		},
	}
	bundle := chunker.SemanticBundle{Name: "challenger-audit", PrimaryFiles: []string{"src/a.cpp"}, AllFiles: []string{"src/a.cpp"}}
	output, _, err := (&DebateEngine{}).runChallengerStage(
		engineCtx,
		bundle,
		&HunterOutput{Candidates: candidates},
		filepath.Join(tempDir, "challenger.json"),
	)
	if err != nil {
		t.Fatalf("runChallengerStage() error = %v", err)
	}
	return output, models.DB
}

func challengerAuditCandidates(count int) []HunterCandidate {
	candidates := make([]HunterCandidate, 0, count)
	for index := 0; index < count; index++ {
		candidates = append(candidates, HunterCandidate{
			CandidateID:      fmt.Sprintf("H-%03d", index+1),
			FilePath:         "src/a.cpp",
			LineRange:        "1-2",
			TriggerLine:      "if (!value) return;",
			ScopeSymbol:      "check",
			Category:         "测试分类",
			Title:            fmt.Sprintf("candidate %d", index+1),
			CodeSnippet:      "if (!value) return;",
			TriggerCondition: "value is empty",
		})
	}
	return candidates
}

func validChallengerArtifact(count int) string {
	return validChallengerArtifactFrom(1, count)
}

func validChallengerArtifactFrom(start int, count int) string {
	cases := make([]string, 0, count)
	for index := 0; index < count; index++ {
		candidateID := start + index
		cases = append(cases, fmt.Sprintf(`{"candidate_id":"H-%03d","defense_verdict":"DEFENSE_SUCCESSFUL","defense_arguments":[{"dimension":"Guards","finding":"outer guard exists","evidence":[{"evidence_id":"H-%03d-target","kind":"target_source","path":"src/a.cpp","line_range":"1-2","snippet":"if (!value) return;\nreturn value;","reason":"Candidate anchor and surrounding source context."}]}],"mitigating_factors":"guard present"}`, candidateID, candidateID))
	}
	return `{"schema":"` + DefenseCasesArtifactSchemaV2 + `","defense_cases":[` + joinStrings(cases, ",") + `]}`
}

func minimalChallengerArtifact(count int) string {
	cases := make([]string, 0, count)
	for index := 0; index < count; index++ {
		cases = append(cases, fmt.Sprintf(`{"candidate_id":"H-%03d","defense_verdict":"DEFENSE_SUCCESSFUL","defense_arguments":[{"dimension":"Guards","finding":"outer guard exists","evidence":[{"evidence_id":"H-%03d-target","kind":"target_source","reason":"source evidence"}]}],"mitigating_factors":"guard present"}`, index+1, index+1))
	}
	return `{"schema":"` + DefenseCasesArtifactSchemaV2 + `","defense_cases":[` + joinStrings(cases, ",") + `]}`
}

func joinStrings(values []string, separator string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += separator
		}
		result += value
	}
	return result
}

func queryChallengerAudits(t *testing.T, db *gorm.DB) []models.ArtifactRepairAudit {
	t.Helper()
	var audits []models.ArtifactRepairAudit
	if err := db.Order("id ASC").Find(&audits).Error; err != nil {
		t.Fatalf("query challenger audits: %v", err)
	}
	return audits
}

func decodeChallengerAttempts(t *testing.T, audit models.ArtifactRepairAudit) []ArtifactRepairAttempt {
	t.Helper()
	var attempts []ArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode challenger attempts: %v", err)
	}
	return attempts
}

func TestChallengerAuditDirectPassPersistsSchemaMetrics(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{name: "challenger-audit-direct", initial: validChallengerArtifact(1)}
	db := setupChallengerAudit(t, auditInvoker)
	output, _ := runChallengerAudit(t, challengerAuditCandidates(1))
	if len(output.DefenseCases) != 1 || output.DefenseCases[0].DefenseVerdict != "DEFENSE_SUCCESSFUL" {
		t.Fatalf("business output changed: %#v", output)
	}
	audits := queryChallengerAudits(t, db)
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audits))
	}
	audit := audits[0]
	if audit.BundleID != "challenger-audit/challenger/batch-1" || audit.SchemaID != DefenseCasesArtifactSchemaV2 ||
		audit.SchemaHash == "" || audit.Driver != auditInvoker.name || audit.ResourceID != auditInvoker.name ||
		audit.ResponseFormatMode != "prompt_contract" || audit.FinalStatus != "success" ||
		audit.LocalRepairs != 0 || audit.LLMRepairAttempts != 0 {
		t.Fatalf("audit = %#v", audit)
	}
	attempts := decodeChallengerAttempts(t, audit)
	if len(attempts) != 1 || attempts[0].Source != "direct" || !attempts[0].Accepted || attempts[0].Raw == "" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestChallengerAuditLocalEvidenceNormalization(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{name: "challenger-audit-local", initial: minimalChallengerArtifact(1)}
	db := setupChallengerAudit(t, auditInvoker)
	_, _ = runChallengerAudit(t, challengerAuditCandidates(1))
	audit := queryChallengerAudits(t, db)[0]
	attempts := decodeChallengerAttempts(t, audit)
	if audit.LocalRepairs != 1 || len(attempts) != 2 || attempts[1].Source != "local" ||
		!attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestChallengerAuditLLMRepairSuccessPersistsRawArtifact(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{
		name:    "challenger-audit-llm",
		initial: `{}`,
		repair:  validChallengerArtifact(1),
	}
	db := setupChallengerAudit(t, auditInvoker)
	output, _ := runChallengerAudit(t, challengerAuditCandidates(1))
	if len(output.DefenseCases) != 1 || output.DefenseCases[0].DefenseVerdict != "DEFENSE_SUCCESSFUL" {
		t.Fatalf("business output invalid: %#v", output)
	}
	audit := queryChallengerAudits(t, db)[0]
	attempts := decodeChallengerAttempts(t, audit)
	if audit.FinalStatus != "success" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 1 ||
		len(attempts) != 2 || attempts[1].Source != "llm" || !attempts[1].Accepted ||
		attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestChallengerAuditLLMFailureKeepsFailedAttempt(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{
		name:     "challenger-audit-failed",
		initial:  `{}`,
		repairEr: errors.New("repair unavailable"),
	}
	db := setupChallengerAudit(t, auditInvoker)
	output, _ := runChallengerAudit(t, challengerAuditCandidates(1))
	if len(output.DefenseCases) != 1 || output.DefenseCases[0].DefenseVerdict != "CHALLENGE_FAILED" {
		t.Fatalf("degraded output invalid: %#v", output)
	}
	audit := queryChallengerAudits(t, db)[0]
	attempts := decodeChallengerAttempts(t, audit)
	if audit.FinalStatus != "failed" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 0 ||
		len(attempts) != 2 || attempts[1].Accepted || attempts[1].Raw != "" || attempts[1].Error == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestChallengerAuditRecordsDriftedRepair(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{
		name:    "challenger-audit-drift",
		initial: `{}`,
		repair:  validChallengerArtifact(1),
	}
	db := setupChallengerAudit(t, auditInvoker)
	_, _ = runChallengerAudit(t, challengerAuditCandidates(1))
	audit := queryChallengerAudits(t, db)[0]
	attempts := decodeChallengerAttempts(t, audit)
	if !audit.RepairDrifted || audit.RepairDriftUnitRef != "H-001" || len(attempts) != 2 ||
		!attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestChallengerAuditRecordsUncheckedDrift(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{
		name:    "challenger-audit-unchecked",
		initial: `not json`,
		repair:  validChallengerArtifact(1),
	}
	db := setupChallengerAudit(t, auditInvoker)
	_, _ = runChallengerAudit(t, challengerAuditCandidates(1))
	audit := queryChallengerAudits(t, db)[0]
	attempts := decodeChallengerAttempts(t, audit)
	if !audit.RepairDriftUnchecked || len(attempts) != 2 || !attempts[1].Accepted {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestChallengerAuditPersistsEveryBatch(t *testing.T) {
	auditInvoker := &challengerAuditInvoker{
		name:    "challenger-audit-batches",
		initial: validChallengerArtifactFrom(1, 6),
		outputByCall: map[int]string{
			2: validChallengerArtifactFrom(1, 3),
			4: validChallengerArtifactFrom(4, 3),
		},
	}
	db := setupChallengerAudit(t, auditInvoker)
	output, _ := runChallengerAudit(t, challengerAuditCandidates(6))
	if len(output.DefenseCases) != 6 {
		t.Fatalf("business output count = %d, want 6", len(output.DefenseCases))
	}
	audits := queryChallengerAudits(t, db)
	if len(audits) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(audits))
	}
	for index, audit := range audits {
		want := fmt.Sprintf("challenger-audit/challenger/batch-%d", index+1)
		if audit.BundleID != want || audit.FinalStatus != "success" {
			t.Fatalf("audit[%d] = %#v, want bundle %s", index, audit, want)
		}
	}
}
