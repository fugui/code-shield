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

type judgeAuditInvoker struct {
	name         string
	initial      string
	repair       string
	repairErr    error
	outputByCall map[int]string
	calls        int
}

func (inv *judgeAuditInvoker) Name() string { return inv.name }

func (inv *judgeAuditInvoker) Invoke(request invoker.AIRequest) error {
	inv.calls++
	payload := inv.initial
	if inv.calls > 1 {
		payload = inv.repair
	}
	if byCall, ok := inv.outputByCall[inv.calls]; ok {
		payload = byCall
	}
	if inv.calls > 1 && inv.repairErr != nil {
		return inv.repairErr
	}
	return os.WriteFile(request.OutputPath, []byte(payload), 0640)
}

func setupJudgeAudit(t *testing.T, auditInvoker *judgeAuditInvoker) *gorm.DB {
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

	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier3Judge
	models.AppConfig.Scanner.Debate.Tiers.Tier3Judge = models.TierBindingConfig{
		Resource:              auditInvoker.Name(),
		Resources:             []string{auditInvoker.Name()},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		MaxSplitDepth:         1,
		Recovery:              &models.TierRecoveryConfig{ContractRepairOn: []string{"contract_mismatch"}},
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Debate.Tiers.Tier3Judge = previousTier })
	return db
}

func runJudgeAudit(t *testing.T, candidates []HunterCandidate) (*JudgeOutput, *gorm.DB) {
	t.Helper()
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "a.cpp"), []byte("if (!value) return;\nunsafe();\nreturn value;\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	engineCtx := &engines.EngineContext{
		Ctx:               beginSplitStats(context.Background()),
		ReportID:          43,
		RepoID:            8,
		TaskTypeID:        10,
		CodesPath:         tempDir,
		EngineMode:        "debate_full",
		AllowedCategories: []string{"并发安全"},
	}
	bundle := chunker.SemanticBundle{Name: "judge-audit", PrimaryFiles: []string{"src/a.cpp"}, AllFiles: []string{"src/a.cpp"}}
	output, _, err := (&DebateEngine{}).runJudgeStage(
		engineCtx,
		bundle,
		&HunterOutput{Candidates: candidates},
		&ChallengerOutput{},
		filepath.Join(tempDir, "judge.json"),
	)
	if err != nil {
		t.Fatalf("runJudgeStage() error = %v", err)
	}
	return output, models.DB
}

func judgeAuditCandidates(count int) []HunterCandidate {
	candidates := make([]HunterCandidate, 0, count)
	for index := 0; index < count; index++ {
		candidates = append(candidates, HunterCandidate{
			CandidateID:      fmt.Sprintf("H-%03d", index+1),
			FilePath:         "src/a.cpp",
			LineRange:        "1-3",
			TriggerLine:      "unsafe();",
			ScopeSymbol:      "run",
			Category:         "并发安全",
			Title:            fmt.Sprintf("candidate %d", index+1),
			CodeSnippet:      "unsafe();",
			TriggerCondition: "unsafe call is reachable",
		})
	}
	return candidates
}

func validJudgeArtifactFrom(start int, count int, verdict string) string {
	verdicts := make([]string, 0, count)
	for index := 0; index < count; index++ {
		candidateID := start + index
		verdicts = append(verdicts, fmt.Sprintf(`{
			"candidate_id":"H-%03d",
			"verdict":%q,
			"severity_preliminary":"一般",
			"category":"并发安全",
			"file_path":"src/a.cpp",
			"line_range":"1-3",
			"trigger_line":"unsafe();",
			"scope_symbol":"run",
			"title":"unsafe call",
			"judgement_rationale":"source confirms unsafe call",
			"code_snippet":"unsafe();",
			"suggestion":"add guard",
			"evidence":[{"evidence_id":"H-%03d-target","kind":"target","path":"src/a.cpp","line_range":"1-3","snippet":"unsafe();","reason":"target source confirms"}]
		}`, candidateID, verdict, candidateID))
	}
	return `{"schema":"` + FinalVerdictsArtifactSchemaV2 + `","final_verdicts":[` + joinStrings(verdicts, ",") + `]}`
}

func validJudgeArtifact(count int) string {
	return validJudgeArtifactFrom(1, count, "CONDITIONAL")
}

func mismatchedValidJudgeArtifact() string {
	return replaceOnce(validJudgeArtifact(1), `"schema":"`+FinalVerdictsArtifactSchemaV2+`"`, `"schema":"wrong"`)
}

func evidenceDriftJudgeArtifact() string {
	artifact := validJudgeArtifact(1)
	artifact = replaceOnce(artifact, `"evidence_id":"H-001-target"`, `"evidence_id":"H-001-alternate-target"`)
	return replaceOnce(artifact, `"kind":"target"`, `"kind":"target_source"`)
}

func failedNormalizedJudgeRepairArtifact() string {
	artifact := validJudgeArtifact(1)
	return replaceOnce(artifact, `"verdict":"CONDITIONAL"`, `"verdict":"BOGUS"`)
}

func businessDriftJudgeArtifact() string {
	return validJudgeArtifactFrom(1, 1, "REJECTED")
}

func replaceOnce(value, old, new string) string {
	for index := 0; index+len(old) <= len(value); index++ {
		if value[index:index+len(old)] == old {
			return value[:index] + new + value[index+len(old):]
		}
	}
	return value
}

func queryJudgeAudits(t *testing.T, db *gorm.DB) []models.ArtifactRepairAudit {
	t.Helper()
	var audits []models.ArtifactRepairAudit
	if err := db.Order("id ASC").Find(&audits).Error; err != nil {
		t.Fatalf("query judge audits: %v", err)
	}
	return audits
}

func decodeJudgeAttempts(t *testing.T, audit models.ArtifactRepairAudit) []ArtifactRepairAttempt {
	t.Helper()
	var attempts []ArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode judge attempts: %v", err)
	}
	return attempts
}

func TestJudgeAuditDirectPassPersistsSchemaMetrics(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-direct", initial: validJudgeArtifact(1)}
	db := setupJudgeAudit(t, auditInvoker)
	output, _ := runJudgeAudit(t, judgeAuditCandidates(1))
	if len(output.FinalVerdicts) != 1 || output.FinalVerdicts[0].Verdict != "CONDITIONAL" {
		t.Fatalf("business output changed: %#v", output)
	}
	audits := queryJudgeAudits(t, db)
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audits))
	}
	audit := audits[0]
	if audit.BundleID != "judge-audit/judge/batch-1" || audit.SchemaID != FinalVerdictsArtifactSchemaV2 ||
		audit.SchemaHash == "" || audit.Driver != auditInvoker.name || audit.ResourceID != auditInvoker.name ||
		audit.ResponseFormatMode != "prompt_contract" || audit.FinalStatus != "success" ||
		audit.OriginalArtifactHash == "" || audit.LocalRepairs != 1 || audit.LLMRepairAttempts != 0 {
		t.Fatalf("audit = %#v", audit)
	}
	attempts := decodeJudgeAttempts(t, audit)
	if len(attempts) != 2 || attempts[0].Source != "direct" || !attempts[0].Accepted ||
		attempts[0].Tokens <= 0 || attempts[0].Raw == "" {
		t.Fatalf("attempts = %#v", attempts)
	}
	if attempts[1].Source != "local" || !attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("local attempt = %#v", attempts[1])
	}
}

func TestJudgeAuditLLMRepairSuccessPersistsRawArtifact(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-llm", initial: mismatchedValidJudgeArtifact(), repair: validJudgeArtifact(1)}
	db := setupJudgeAudit(t, auditInvoker)
	output, _ := runJudgeAudit(t, judgeAuditCandidates(1))
	if len(output.FinalVerdicts) != 1 || output.FinalVerdicts[0].Verdict != "CONDITIONAL" {
		t.Fatalf("business output invalid: %#v", output)
	}
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if audit.FinalStatus != "success" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 1 ||
		len(attempts) != 3 || attempts[1].Source != "llm" || attempts[1].Tokens <= 0 ||
		!attempts[1].Accepted || attempts[1].NonRetryable || attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
	if attempts[2].Source != "local" || !attempts[2].Accepted || attempts[2].Raw == "" {
		t.Fatalf("local retry attempt = %#v", attempts[2])
	}
}

func TestJudgeAuditLLMFailureKeepsFailedAttempt(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-failed", initial: `{}`, repairErr: errors.New("repair unavailable")}
	db := setupJudgeAudit(t, auditInvoker)
	output, _ := runJudgeAudit(t, judgeAuditCandidates(1))
	if len(output.FinalVerdicts) != 1 || output.FinalVerdicts[0].Verdict != "CONDITIONAL" || !output.FinalVerdicts[0].EvidenceDegraded {
		t.Fatalf("degraded output invalid: %#v", output)
	}
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if audit.FinalStatus != "failed" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 0 ||
		len(attempts) != 2 || attempts[1].Accepted || attempts[1].Error == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestJudgeAuditRecordsBusinessDriftedRepair(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-drift", initial: mismatchedValidJudgeArtifact(), repair: businessDriftJudgeArtifact()}
	db := setupJudgeAudit(t, auditInvoker)
	_, _ = runJudgeAudit(t, judgeAuditCandidates(1))
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if !audit.RepairDrifted || audit.RepairDriftUnitRef != "H-001" || audit.RepairDriftUnchecked || len(attempts) != 3 ||
		!attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestJudgeAuditRecordsEvidenceDriftedRepair(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-evidence-drift", initial: mismatchedValidJudgeArtifact(), repair: evidenceDriftJudgeArtifact()}
	db := setupJudgeAudit(t, auditInvoker)
	_, _ = runJudgeAudit(t, judgeAuditCandidates(1))
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if !audit.RepairDrifted || audit.RepairDriftUnitRef != "H-001" || audit.RepairDriftUnchecked || len(attempts) != 2 || !attempts[1].Accepted {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
	var issues []ArtifactIssue
	if err := json.Unmarshal(audit.SchemaRepairIssues, &issues); err != nil {
		t.Fatalf("decode issues: %v", err)
	}
	foundEvidenceDrift := false
	for _, issue := range issues {
		if issue.Field == "evidence" && issue.Message == "JUDGE_EVIDENCE_DRIFT on repair attempt" {
			foundEvidenceDrift = true
		}
	}
	if !foundEvidenceDrift {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestJudgeAuditFailedRepairKeepsNormalizedRetry(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-failed-normalized", initial: mismatchedValidJudgeArtifact(), repair: failedNormalizedJudgeRepairArtifact()}
	db := setupJudgeAudit(t, auditInvoker)
	_, _ = runJudgeAudit(t, judgeAuditCandidates(1))
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if audit.FinalStatus != "failed" || len(attempts) != 3 ||
		attempts[1].Source != "llm" || attempts[1].Accepted ||
		attempts[2].Source != "local" || attempts[2].Accepted || attempts[2].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestJudgeAuditRecordsUncheckedBaseline(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{name: "judge-audit-unchecked", initial: "not json", repair: validJudgeArtifact(1)}
	db := setupJudgeAudit(t, auditInvoker)
	_, _ = runJudgeAudit(t, judgeAuditCandidates(1))
	audit := queryJudgeAudits(t, db)[0]
	attempts := decodeJudgeAttempts(t, audit)
	if !audit.RepairDriftUnchecked || len(attempts) != 3 || !attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestJudgeAuditPersistsEveryBatch(t *testing.T) {
	auditInvoker := &judgeAuditInvoker{
		name: "judge-audit-batches",
		outputByCall: map[int]string{
			1: validJudgeArtifact(3),
			2: validJudgeArtifactFrom(4, 3, "CONDITIONAL"),
		},
	}
	db := setupJudgeAudit(t, auditInvoker)
	output, _ := runJudgeAudit(t, judgeAuditCandidates(6))
	if len(output.FinalVerdicts) != 6 {
		t.Fatalf("business output count = %d, want 6", len(output.FinalVerdicts))
	}
	audits := queryJudgeAudits(t, db)
	if len(audits) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(audits))
	}
	for index, audit := range audits {
		want := fmt.Sprintf("judge-audit/judge/batch-%d", index+1)
		if audit.BundleID != want || audit.FinalStatus != "success" {
			t.Fatalf("audit[%d] = %#v, want bundle %s", index, audit, want)
		}
	}
}
