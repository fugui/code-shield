package debate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
	"code-shield/services/engines/chunker"
	"code-shield/services/invoker"
	"gorm.io/gorm"
)

type auditNormalizer struct{}

func (auditNormalizer) Normalize(raw string, _ *assessment.PromptRefSession) (assessment.AssessmentArtifact, error) {
	var artifact assessment.AssessmentArtifact
	err := json.Unmarshal([]byte(raw), &artifact)
	return artifact, err
}

type auditValidator struct{}

func (auditValidator) Validate(_ assessment.PlanView, artifact assessment.AssessmentArtifact) (assessment.AssessmentResult, error) {
	for _, item := range artifact.Assessments {
		if strings.TrimSpace(item.Summary) == "" {
			return assessment.AssessmentResult{}, errors.New("summary is required")
		}
	}
	return assessment.AssessmentResult{Artifact: artifact, Valid: artifact.Assessments}, nil
}

type auditReconciler struct{}

func (auditReconciler) Reconcile(_ assessment.PlanView, result assessment.AssessmentResult) assessment.UnitReconciliation {
	return assessment.UnitReconciliation{
		PlanReconciliation: coverage.PlanReconciliation{
			PlannedUnits: len(result.Valid),
			MatchedUnits: len(result.Valid),
		},
	}
}

type auditFindingMapper struct{}

func (auditFindingMapper) MapFindings(ctx assessment.AssessmentContext, _ assessment.Bundle, result assessment.AssessmentResult) ([]models.AnalysisFinding, error) {
	findings := make([]models.AnalysisFinding, 0, len(result.Valid))
	for _, item := range result.Valid {
		findings = append(findings, models.AnalysisFinding{
			TaskReportID:     ctx.EngineContext.ReportID,
			TaskTypeID:       ctx.EngineContext.TaskTypeID,
			RepoID:           ctx.EngineContext.RepoID,
			PrimaryUnitID:    item.PrimaryUnitID,
			Severity:         "一般",
			Title:            "assessment finding",
			AssessmentStatus: string(item.Outcome),
		})
	}
	return findings, nil
}

type auditStatusMapper struct{}

func (auditStatusMapper) OutcomeForStatus(status string) (assessment.AssessmentOutcome, bool) {
	return assessment.AssessmentOutcome(status), true
}

func (auditStatusMapper) OutcomeForArtifact(artifact assessment.AssessmentArtifact) (assessment.AssessmentOutcome, bool) {
	if len(artifact.Assessments) == 0 {
		return "", false
	}
	return artifact.Assessments[0].Outcome, true
}

type scriptedRepairInvoker struct {
	response string
	err      error
}

func (invoker *scriptedRepairInvoker) Name() string { return "native" }

func (invoker *scriptedRepairInvoker) Invoke(request invoker.AIRequest) error {
	if invoker.err != nil {
		return invoker.err
	}
	return os.WriteFile(request.OutputPath, []byte(invoker.response), 0640)
}

func auditRegistration() assessment.ProfileRegistration {
	return assessment.ProfileRegistration{
		Descriptor:  assessment.Descriptor{Name: "audit-test"},
		Normalize:   auditNormalizer{},
		Validate:    auditValidator{},
		Reconcile:   auditReconciler{},
		MapFindings: auditFindingMapper{},
		Outcome:     auditStatusMapper{},
	}
}

func auditTestContext(bundle string) (*engines.EngineContext, assessment.PromptRefSession, error) {
	session, err := assessment.NewPromptRefSession(bundle, []string{"unit-1", "unit-2"}, nil)
	if err != nil {
		return nil, assessment.PromptRefSession{}, err
	}
	return &engines.EngineContext{
		Ctx:        beginSplitStats(context.Background()),
		ReportID:   42,
		RepoID:     7,
		TaskTypeID: 9,
	}, *session, nil
}

func enableAuditRepair(t *testing.T) {
	t.Helper()
	enabled := true
	previousEnabled := models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled
	previousResource := models.AppConfig.Scanner.Artifact.SchemaRepairResource
	previousAttempts := models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &enabled
	models.AppConfig.Scanner.Artifact.SchemaRepairResource = "native"
	models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts = 1
	t.Cleanup(func() {
		models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = previousEnabled
		models.AppConfig.Scanner.Artifact.SchemaRepairResource = previousResource
		models.AppConfig.Scanner.Artifact.MaxSchemaRepairAttempts = previousAttempts
	})
}

func assessmentContract(t *testing.T) assessment.ArtifactContract {
	t.Helper()
	contract, err := assessment.ArtifactContractForOutput(assessment.AssessmentStage, assessment.OutputContract{
		SchemaID:          assessment.AssessmentsArtifactSchemaV2,
		TopLevel:          "assessments",
		RequiredFields:    []string{"schema", "assessments"},
		AllowedOutcomes:   []string{"pass", "defect", "not_target", "needs_human"},
		AllowedCategories: []string{"内存安全"},
	}, "")
	if err != nil {
		t.Fatalf("ArtifactContractForOutput() error = %v", err)
	}
	return contract
}

func setupAuditTest(t *testing.T, repairInvoker *scriptedRepairInvoker) (
	*gorm.DB,
	*engines.EngineContext,
	assessment.PromptRefSession,
	assessment.ProfileRegistration,
	assessment.ArtifactContract,
) {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, "shield_assessment_repair_audit", &models.AnalysisFinding{}, &models.ArtifactRepairAudit{})
	models.DB = db
	t.Cleanup(func() { models.DB = nil })
	invoker.RegisterAIInvoker("native", repairInvoker)
	enableAuditRepair(t)
	engineCtx, session, err := auditTestContext("audit-bundle")
	if err != nil {
		t.Fatalf("NewPromptRefSession() error = %v", err)
	}
	registration := auditRegistration()
	return db, engineCtx, session, registration, assessmentContract(t)
}

func lastAudit(t *testing.T, db *gorm.DB) models.ArtifactRepairAudit {
	t.Helper()
	var audit models.ArtifactRepairAudit
	if err := db.Order("id DESC").First(&audit).Error; err != nil {
		t.Fatalf("query artifact repair audit: %v", err)
	}
	return audit
}

func TestConsumePluginArtifactPersistsSuccessAudit(t *testing.T) {
	repairInvoker := &scriptedRepairInvoker{}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	raw := `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"defect","summary":"two"}]}`
	observation := assessment.ArtifactPipelineObservation{
		Driver:                  "native",
		ResourceID:              "resource-1",
		ResponseFormatMode:      "json_object",
		ResponseFormatFallbacks: 1,
	}
	findings, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract, observation,
	)
	if err != nil {
		t.Fatalf("consumePluginArtifact() error = %v", err)
	}
	if len(findings) != 2 || findings[0].ArtifactSchemaID != contract.SchemaID ||
		findings[0].ArtifactSchemaHash != contract.SchemaHash || len(findings[0].ArtifactRepairMetrics) == 0 {
		t.Fatalf("findings audit fields = %#v", findings)
	}
	audit := lastAudit(t, db)
	if audit.FinalStatus != "success" || audit.SchemaID != contract.SchemaID || audit.SchemaHash != contract.SchemaHash {
		t.Fatalf("audit = %#v", audit)
	}
	stats := splitStatsFromContext(engineCtx.Ctx)
	if stats == nil || stats.ArtifactSchemaID != contract.SchemaID || stats.ArtifactSchemaHash != contract.SchemaHash ||
		stats.ResponseFormatMode != "json_object" || stats.ResponseFormatFallbacks != 1 {
		t.Fatalf("assessment observation stats = %+v", stats)
	}
}

func TestConsumePluginArtifactPersistsLocalRepairAttempt(t *testing.T) {
	repairInvoker := &scriptedRepairInvoker{}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	raw := `{"schema":"code-shield.unit-assessments.v1","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	_, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract,
		assessment.ArtifactPipelineObservation{Driver: "native", ResourceID: "resource-1", ResponseFormatMode: "off"},
	)
	if err != nil {
		t.Fatalf("consumePluginArtifact() error = %v", err)
	}
	audit := lastAudit(t, db)
	if audit.LocalRepairs != 1 || audit.LLMRepairAttempts != 0 || audit.FinalStatus != "success" {
		t.Fatalf("audit = %#v", audit)
	}
	assertAuditAttempts(t, audit, "u001")
}

func TestConsumePluginArtifactPersistsLLMRepairAttempt(t *testing.T) {
	repaired := `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	repairInvoker := &scriptedRepairInvoker{response: repaired}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	raw := `{"schema":"legacy","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	_, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract,
		assessment.ArtifactPipelineObservation{Driver: "native", ResourceID: "resource-1", ResponseFormatMode: "off"},
	)
	if err != nil {
		t.Fatalf("consumePluginArtifact() error = %v", err)
	}
	audit := lastAudit(t, db)
	if audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 1 || audit.RepairTokens <= 0 || audit.FinalStatus != "success" {
		t.Fatalf("audit = %#v", audit)
	}
	assertAuditAttempts(t, audit, "u002")
}

func TestConsumePluginArtifactPersistsFailedLLMAttempt(t *testing.T) {
	repairInvoker := &scriptedRepairInvoker{err: errors.New("repair unavailable")}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	raw := `{"schema":"legacy","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	_, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract,
		assessment.ArtifactPipelineObservation{Driver: "native", ResourceID: "resource-1", ResponseFormatMode: "off"},
	)
	if err == nil {
		t.Fatal("consumePluginArtifact() should fail")
	}
	audit := lastAudit(t, db)
	if audit.FinalStatus != "failed" || audit.LLMRepairAttempts != 1 || audit.LLMRepairSuccesses != 0 {
		t.Fatalf("audit = %#v", audit)
	}
	var attempts []assessment.ArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode attempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Accepted || attempts[0].Raw != "" || attempts[0].Error == "" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestConsumePluginArtifactPersistsDriftedAttempt(t *testing.T) {
	drifted := `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"defect","summary":"changed-one"},{"unit_ref":"u002","outcome":"defect","summary":"changed-two"}]}`
	repairInvoker := &scriptedRepairInvoker{response: drifted}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	raw := `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	_, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract,
		assessment.ArtifactPipelineObservation{Driver: "native", ResourceID: "resource-1", ResponseFormatMode: "off"},
	)
	if err == nil {
		t.Fatal("consumePluginArtifact() should reject drifted repair")
	}
	audit := lastAudit(t, db)
	if !audit.RepairDrifted || audit.RepairDriftUnitRef == "" || audit.FinalStatus != "failed" {
		t.Fatalf("audit = %#v", audit)
	}
	assertAuditAttempts(t, audit, "changed-one")
}

func TestConsumePluginArtifactDisabledLeavesAuditFieldsEmpty(t *testing.T) {
	repairInvoker := &scriptedRepairInvoker{response: "{broken"}
	db, engineCtx, session, registration, contract := setupAuditTest(t, repairInvoker)
	previous := models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled
	disabled := false
	models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = &disabled
	t.Cleanup(func() { models.AppConfig.Scanner.Artifact.AssessmentContractRepairEnabled = previous })
	raw := `{"schema":"code-shield.unit-assessments.v2","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"one"},{"unit_ref":"u002","outcome":"pass","summary":"two"}]}`
	findings, _, _, err := (&DebateEngine{}).consumePluginArtifact(
		engineCtx, chunker.SemanticBundle{Name: "audit-bundle"}, raw, 0, &session, registration, contract,
		assessment.ArtifactPipelineObservation{},
	)
	if err != nil {
		t.Fatalf("consumePluginArtifact() error = %v", err)
	}
	if len(findings) != 2 || findings[0].ArtifactSchemaID != "" || findings[0].ArtifactSchemaHash != "" || findings[0].ArtifactRepairMetrics != nil {
		t.Fatalf("findings = %#v, want empty audit fields", findings)
	}
	var count int64
	if err := db.Model(&models.ArtifactRepairAudit{}).Count(&count).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if count != 0 {
		t.Fatalf("audit rows = %d, want 0", count)
	}
}

func assertAuditAttempts(t *testing.T, audit models.ArtifactRepairAudit, want string) {
	t.Helper()
	var attempts []assessment.ArtifactRepairAttempt
	if err := json.Unmarshal(audit.RepairAttempts, &attempts); err != nil {
		t.Fatalf("decode attempts: %v", err)
	}
	if len(attempts) != 1 || !strings.Contains(attempts[0].Raw, want) {
		t.Fatalf("attempts = %#v, want raw containing %q", attempts, want)
	}
}
