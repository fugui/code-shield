package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/engines/debate"
	"code-shield/services/invoker"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type synthesisAuditInvoker struct {
	name         string
	outputByCall map[int]string
	failByCall   map[int]error
	calls        int
}

func (inv *synthesisAuditInvoker) Name() string { return inv.name }

func (inv *synthesisAuditInvoker) Invoke(request invoker.AIRequest) error {
	inv.calls++
	if invocationErr, ok := inv.failByCall[inv.calls]; ok {
		return invocationErr
	}
	output := inv.outputByCall[inv.calls]
	return os.WriteFile(request.OutputPath, []byte(output), 0640)
}

func setupSynthesisAudit(t *testing.T, auditInvoker *synthesisAuditInvoker) *gorm.DB {
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

	previousTier := models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis
	invoker.RegisterAIInvoker(auditInvoker.Name(), auditInvoker)
	models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis = models.TierBindingConfig{
		Resource:              auditInvoker.Name(),
		Resources:             []string{auditInvoker.Name()},
		TimeoutSeconds:        10,
		AttemptTimeoutSeconds: 10,
		Recovery: &models.TierRecoveryConfig{
			MaxTotalAttempts:       2,
			MaxAttemptsPerResource: 2,
			RetryBackoffMs:         1,
		},
	}
	t.Cleanup(func() { models.AppConfig.Scanner.Debate.Tiers.Tier4Synthesis = previousTier })
	return db
}

func writeSynthesisAuditPrompt(t *testing.T, taskTypeName string) {
	t.Helper()
	promptDir := filepath.Join(models.GetAppBaseDir(), "tasks", taskTypeName)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatalf("create prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "synthesis_prompt.md")
	if err := os.WriteFile(promptPath, []byte("Generate the Tier 4 report."), 0644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(promptPath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove prompt: %v", err)
		}
		_ = os.Remove(promptDir)
	})
}

func querySynthesisAudits(t *testing.T, db *gorm.DB) []models.ArtifactRepairAudit {
	t.Helper()
	var audits []models.ArtifactRepairAudit
	if err := db.Order("id ASC").Find(&audits).Error; err != nil {
		t.Fatalf("query synthesis audits: %v", err)
	}
	return audits
}

func decodeSynthesisAttempts(t *testing.T, audit models.ArtifactRepairAudit) []debate.ArtifactRepairAttempt {
	t.Helper()
	var attempts []debate.ArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode synthesis attempts: %v", err)
	}
	return attempts
}

func validSynthesisReport() string {
	return `# Code-Shield 全仓态势汇总报告

## 一、检视结果概要

致命：1，严重：1，一般：0，建议：0

## 二、重点问题

### 1. null deref

- **文件**: src/a.cpp

### 2. index overflow

- **文件**: src/b.cpp
`
}

func driftedSynthesisReport() string {
	return `# Code-Shield 全仓态势汇总报告

## 一、检视结果概要

致命：1，严重：1，一般：0，建议：0

## 二、重点问题

### 1. changed semantic title

- **文件**: src/a.cpp

### 2. index overflow

- **文件**: src/b.cpp
`
}

func TestSynthesisAuditDirectPassPersistsRawAndLocalRepair(t *testing.T) {
	wrapped, err := json.Marshal(map[string]string{"report": validSynthesisReport()})
	if err != nil {
		t.Fatalf("marshal wrapped report: %v", err)
	}
	polished := string(wrapped)
	auditInvoker := &synthesisAuditInvoker{
		name:         "synthesis-audit-direct",
		outputByCall: map[int]string{1: polished},
	}
	db := setupSynthesisAudit(t, auditInvoker)
	writeSynthesisAuditPrompt(t, "synthesis-audit-test")
	tempDir := t.TempDir()
	ctx := &TaskContext{
		Ctx:        context.Background(),
		Report:     models.TaskReport{ID: 44, RepoID: 7, TaskTypeID: 9},
		TaskType:   models.TaskType{Name: "synthesis_audit_test", DisplayName: "安全审计", Timeout: 1},
		Repo:       models.Repository{Name: "demo/repo"},
		ReportPath: filepath.Join(tempDir, "report.md"),
	}
	findings := []models.AnalysisFinding{
		{Severity: "致命", FilePath: "src/a.cpp", LineNumber: "10", Title: "null deref"},
		{Severity: "严重", FilePath: "src/b.cpp", LineNumber: "20", Title: "index overflow"},
	}
	if err := ExecuteSynthesis(ctx, findings); err != nil {
		t.Fatalf("ExecuteSynthesis() error = %v", err)
	}
	if ctx.Summary.Synthesis.Status != "success" {
		t.Fatalf("synthesis status = %q", ctx.Summary.Synthesis.Status)
	}
	content, err := os.ReadFile(ctx.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if strings.TrimSpace(string(content)) != strings.TrimSpace(validSynthesisReport()) {
		t.Fatalf("business output changed:\n%s", content)
	}
	audits := querySynthesisAudits(t, db)
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(audits))
	}
	audit := audits[0]
	if audit.BundleID != "report-44/synthesis" || audit.SchemaID != debate.SynthesisReportArtifactSchema ||
		audit.SchemaHash == "" || audit.Driver != auditInvoker.name || audit.ResourceID != auditInvoker.name ||
		audit.ResponseFormatMode != "prompt_contract" || audit.FinalStatus != "success" ||
		audit.LocalRepairs != 1 || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 1 ||
		audit.RepairDrifted || audit.RepairDriftUnchecked {
		t.Fatalf("audit = %#v", audit)
	}
	attempts := decodeSynthesisAttempts(t, audit)
	if len(attempts) != 2 || attempts[0].Source != "llm" || !attempts[0].Accepted ||
		attempts[0].Tokens <= 0 || attempts[0].Raw == "" ||
		attempts[1].Source != "local" || !attempts[1].Accepted || attempts[1].Raw == "" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestSynthesisAuditLLMFailurePersistsFailedAttempt(t *testing.T) {
	auditInvoker := &synthesisAuditInvoker{
		name:       "synthesis-audit-failed",
		failByCall: map[int]error{1: errors.New("synthesis unavailable")},
	}
	db := setupSynthesisAudit(t, auditInvoker)
	writeSynthesisAuditPrompt(t, "synthesis-audit-test")
	tempDir := t.TempDir()
	ctx := &TaskContext{
		Ctx:        context.Background(),
		Report:     models.TaskReport{ID: 45, RepoID: 7, TaskTypeID: 9},
		TaskType:   models.TaskType{Name: "synthesis-audit-test", DisplayName: "安全审计", Timeout: 1},
		Repo:       models.Repository{Name: "demo/repo"},
		ReportPath: filepath.Join(tempDir, "report.md"),
	}
	findings := []models.AnalysisFinding{
		{Severity: "致命", FilePath: "src/a.cpp", Title: "null deref"},
		{Severity: "严重", FilePath: "src/b.cpp", Title: "index overflow"},
	}
	if err := ExecuteSynthesis(ctx, findings); err != nil {
		t.Fatalf("ExecuteSynthesis should degrade, not fail: %v", err)
	}
	if ctx.Summary.Synthesis.Status != "degraded" {
		t.Fatalf("synthesis status = %q", ctx.Summary.Synthesis.Status)
	}
	content, err := os.ReadFile(ctx.ReportPath)
	if err != nil {
		t.Fatalf("read fallback report: %v", err)
	}
	if !strings.Contains(string(content), "null deref") || !strings.Contains(string(content), "index overflow") {
		t.Fatalf("fallback report lost findings:\n%s", content)
	}
	audit := querySynthesisAudits(t, db)[0]
	attempts := decodeSynthesisAttempts(t, audit)
	if audit.FinalStatus != "failed" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 0 ||
		!audit.RepairDriftUnchecked || len(attempts) != 1 || attempts[0].Accepted ||
		attempts[0].Error == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestSynthesisAuditRecordsDriftedPolish(t *testing.T) {
	auditInvoker := &synthesisAuditInvoker{
		name:         "synthesis-audit-drift",
		outputByCall: map[int]string{1: driftedSynthesisReport()},
	}
	db := setupSynthesisAudit(t, auditInvoker)
	writeSynthesisAuditPrompt(t, "synthesis-audit-test")
	tempDir := t.TempDir()
	ctx := &TaskContext{
		Ctx:        context.Background(),
		Report:     models.TaskReport{ID: 46, RepoID: 7, TaskTypeID: 9},
		TaskType:   models.TaskType{Name: "synthesis-audit-test", DisplayName: "安全审计", Timeout: 1},
		Repo:       models.Repository{Name: "demo/repo"},
		ReportPath: filepath.Join(tempDir, "report.md"),
	}
	findings := []models.AnalysisFinding{
		{Severity: "致命", FilePath: "src/a.cpp", Title: "null deref"},
		{Severity: "严重", FilePath: "src/b.cpp", Title: "index overflow"},
	}
	if err := ExecuteSynthesis(ctx, findings); err != nil {
		t.Fatalf("ExecuteSynthesis() error = %v", err)
	}
	audit := querySynthesisAudits(t, db)[0]
	attempts := decodeSynthesisAttempts(t, audit)
	if audit.FinalStatus != "success" || !audit.RepairDrifted || audit.RepairDriftUnitRef != "detail-0" || audit.RepairDriftUnchecked ||
		len(attempts) != 2 || !attempts[0].Accepted || !attempts[0].Drifted || attempts[0].Raw == "" ||
		!attempts[1].Accepted {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}

func TestSynthesisAuditRecordsUncheckedBaseline(t *testing.T) {
	auditInvoker := &synthesisAuditInvoker{
		name:         "synthesis-audit-unchecked",
		outputByCall: map[int]string{1: "# Plain legacy report"},
	}
	db := setupSynthesisAudit(t, auditInvoker)
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "legacy-input.json")
	if err := os.WriteFile(inputPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write legacy input: %v", err)
	}
	writeSynthesisAuditPrompt(t, "synthesis-audit-test")
	ctx := &TaskContext{
		Ctx:        context.Background(),
		Report:     models.TaskReport{ID: 47, RepoID: 7, TaskTypeID: 9},
		TaskType:   models.TaskType{Name: "synthesis-audit-test", DisplayName: "安全审计", Timeout: 1},
		Repo:       models.Repository{Name: "demo/repo"},
		ReportPath: filepath.Join(tempDir, "report.md"),
	}
	if err := ExecuteSynthesisOnce(ctx, inputPath, ""); err != nil {
		t.Fatalf("ExecuteSynthesisOnce() error = %v", err)
	}
	audit := querySynthesisAudits(t, db)[0]
	attempts := decodeSynthesisAttempts(t, audit)
	if audit.FinalStatus != "success" || audit.RepairDrifted || !audit.RepairDriftUnchecked ||
		len(attempts) != 1 || !attempts[0].Accepted || attempts[0].Raw == "" {
		t.Fatalf("audit=%#v attempts=%#v", audit, attempts)
	}
}
