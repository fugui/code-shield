package defectlifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
	"gorm.io/gorm"
)

func TestPersistScanFactsKeepsReportOnlyAssessmentsOutOfLedger(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "issue.cpp"), []byte("int main() { delete p; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_assessment_gate",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{},
		&models.ScanScopeEntry{}, &models.Defect{}, &models.DefectAlias{},
		&models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "assessment-demo", URL: "https://example.com/assessment-demo.git"}
	taskType := models.TaskType{Name: "specialized-review", DisplayName: "Specialized Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	findings := []models.AnalysisFinding{
		{FilePath: "src/issue.cpp", LineNumber: "1", Title: "unsafe delete", Severity: "严重", TriggerLine: "delete p", ScopeSymbol: "main"},
		{FilePath: "src/valid.cpp", LineNumber: "10", PrimaryUnitID: "entity-valid", AssessmentStatus: "valid", Title: "valid entity", Severity: "合格", Category: "无问题"},
		{FilePath: "src/comment.cpp", LineNumber: "20", PrimaryUnitID: "occ-comment", AssessmentStatus: "not_thread_creation", Title: "comment hit", Severity: "合格", Category: "无问题"},
		{FilePath: "src/safe.cpp", LineNumber: "30", PrimaryUnitID: "change-safe", AssessmentStatus: "SAFE", Title: "safe hunk", Severity: "合格", Category: "无问题"},
		{FilePath: "src/unrelated.cpp", LineNumber: "40", PrimaryUnitID: "change-unrelated", AssessmentStatus: "NOT_CHANGE_RELATED", Title: "unrelated hunk", Severity: "合格", Category: "无问题"},
	}
	scanCoverage := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/issue.cpp", Hash: "hash-issue", Status: coverage.StatusSuccess},
			{Path: "src/valid.cpp", Hash: "hash-valid", Status: coverage.StatusSuccess},
			{Path: "src/comment.cpp", Hash: "hash-comment", Status: coverage.StatusSuccess},
			{Path: "src/safe.cpp", Hash: "hash-safe", Status: coverage.StatusSuccess},
			{Path: "src/unrelated.cpp", Hash: "hash-unrelated", Status: coverage.StatusSuccess},
		},
	}
	result, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: findings, Coverage: scanCoverage,
	})
	if err != nil {
		t.Fatalf("PersistScanFacts() error = %v", err)
	}
	if len(result.Findings) != 3 || len(result.Observations) != 1 {
		t.Fatalf("unexpected result findings=%d observations=%d", len(result.Findings), len(result.Observations))
	}
	var findingCount, observationCount, defectCount int64
	if err := db.Model(&models.AnalysisFinding{}).Where("task_report_id = ?", report.ID).Count(&findingCount).Error; err != nil || findingCount != 3 {
		t.Fatalf("raw findings = %d err=%v, want 3", findingCount, err)
	}
	if err := db.Model(&models.DefectObservation{}).Where("report_id = ?", report.ID).Count(&observationCount).Error; err != nil || observationCount != 1 {
		t.Fatalf("ledger observations = %d err=%v, want 1", observationCount, err)
	}
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("ledger defects = %d err=%v, want 1", defectCount, err)
	}
}

func TestPersistScanFactsResolvesMatchedPassObservation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "int calculate() {\n    return 0;\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "a.cpp"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_pass_resolve",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{},
		&models.ScanScopeEntry{}, &models.Defect{}, &models.DefectAlias{},
		&models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "pass-demo", URL: "https://example.com/pass-demo.git"}
	taskType := models.TaskType{Name: "pass-review", DisplayName: "Pass Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	passFinding := models.AnalysisFinding{
		FilePath: "src/a.cpp", LineNumber: "2", Title: "线程创建点合格：calculate",
		Severity: "合格", Category: "无问题", TriggerLine: "return 0;", ScopeSymbol: "calculate",
		AssessmentStatus: "pass", AssessmentOutcome: "pass",
	}
	identity := BuildIdentities(root, repo.ID, taskType.ID, []models.AnalysisFinding{passFinding})[0]
	lineStart, lineEnd := identity.LineStart, identity.LineEnd
	defect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		StatusReason: "created from NEW observation", CanonicalFingerprint: "legacy-fingerprint",
		IdentityKind: "K1", NormPath: identity.NormPath, ScopeKey: identity.ScopeKey,
		SymbolPath: identity.SymbolPath, StmtShape: identity.StmtShape,
		CleanToken: identity.CleanToken, OccurrenceIndex: identity.OccurrenceIndex,
		DefectClassMajor: "memory_leak", Severity: "严重",
		LineStart: &lineStart, LineEnd: &lineEnd, ScopeBodyHash: identity.ScopeBodyHash,
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	if err := db.Create(&defect).Error; err != nil {
		t.Fatal(err)
	}
	alias := models.DefectAlias{
		DefectID: defect.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		AliasType: AliasPath, AliasValue: identity.NormPath, AliasClass: AliasBucket,
		FirstReportID: report.ID - 1, LastReportID: report.ID - 1, HitCount: 1,
	}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	coverageFact := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/a.cpp", Hash: "hash-a", Status: coverage.StatusSuccess}},
	}
	result, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{passFinding}, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("PersistScanFacts() error = %v", err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Verdict != VerdictCleared {
		t.Fatalf("expected cleared observation, got %+v", result.Observations)
	}
	if result.Observations[0].DefectID == nil || *result.Observations[0].DefectID != defect.ID {
		t.Fatalf("expected defect %d, got %+v", defect.ID, result.Observations[0])
	}

	var updated models.Defect
	if err := db.First(&updated, defect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != DefectStatusResolved || updated.StatusReason != reasonPassCleared {
		t.Fatalf("expected resolved pass defect, got %+v", updated)
	}
	if updated.ResolvedReportID == nil || *updated.ResolvedReportID != report.ID {
		t.Fatalf("expected resolved report %d, got %+v", report.ID, updated.ResolvedReportID)
	}

	var event models.DefectEvent
	if err := db.Where("defect_id = ? AND event_type = ?", defect.ID, EventTypeStatusChanged).
		Order("id DESC").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.FromStatus != DefectStatusActive || event.ToStatus != DefectStatusResolved {
		t.Fatalf("unexpected lifecycle event: %+v", event)
	}

	var defectCount int64
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("defect count = %d err=%v, want 1", defectCount, err)
	}
}

func TestPersistScanFactsWithholdsPassResolutionWhenCoverageDegraded(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.cpp"), []byte("int calculate() {\n    return 0;\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_pass_coverage_gate",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{},
		&models.ScanScopeEntry{}, &models.Defect{}, &models.DefectAlias{},
		&models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "pass-gate-demo", URL: "https://example.com/pass-gate-demo.git"}
	taskType := models.TaskType{Name: "pass-gate-review", DisplayName: "Pass Gate Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	passFinding := models.AnalysisFinding{
		FilePath: "src/a.cpp", LineNumber: "2", Title: "线程创建点合格：calculate",
		Severity: "合格", Category: "无问题", TriggerLine: "return 0;", ScopeSymbol: "calculate",
		AssessmentStatus: "pass", AssessmentOutcome: "pass",
	}
	identity := BuildIdentities(root, repo.ID, taskType.ID, []models.AnalysisFinding{passFinding})[0]
	lineStart, lineEnd := identity.LineStart, identity.LineEnd
	defect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "legacy-fingerprint", IdentityKind: "K1",
		NormPath: identity.NormPath, ScopeKey: identity.ScopeKey,
		SymbolPath: identity.SymbolPath, StmtShape: identity.StmtShape,
		CleanToken: identity.CleanToken, OccurrenceIndex: identity.OccurrenceIndex,
		DefectClassMajor: "memory_leak", Severity: "严重",
		LineStart: &lineStart, LineEnd: &lineEnd, ScopeBodyHash: identity.ScopeBodyHash,
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	if err := db.Create(&defect).Error; err != nil {
		t.Fatal(err)
	}
	alias := models.DefectAlias{
		DefectID: defect.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		AliasType: AliasPath, AliasValue: identity.NormPath, AliasClass: AliasBucket,
		FirstReportID: report.ID - 1, LastReportID: report.ID - 1, HitCount: 1,
	}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}

	coverageFact := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		PlanReconciliation: &coverage.PlanReconciliation{MissingUnits: []string{"missing-unit"}},
		Files:              []coverage.File{{Path: "src/a.cpp", Hash: "hash-a", Status: coverage.StatusSuccess}},
	}
	result, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{passFinding}, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("PersistScanFacts() error = %v", err)
	}
	if len(result.Observations) != 0 {
		t.Fatalf("expected degraded pass observation to be withheld, got %+v", result.Observations)
	}

	var updated models.Defect
	if err := db.First(&updated, defect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status == DefectStatusResolved || updated.ResolvedReportID != nil {
		t.Fatalf("degraded pass must not resolve defect: %+v", updated)
	}
}

func TestAdvanceUnmatchedDefectsWithholdsLifecycleWhenCoverageDegraded(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_coverage_gate",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "coverage-demo", URL: "https://example.com/coverage-demo.git"}
	taskType := models.TaskType{Name: "coverage-review", DisplayName: "Coverage Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	defects := []models.Defect{
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive, CanonicalFingerprint: "fp-active", IdentityKind: "K1", NormPath: "src/a.cpp", Severity: "建议", FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID},
		{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusDormant, CanonicalFingerprint: "fp-dormant", IdentityKind: "K1", NormPath: "src/b.cpp", Severity: "建议", MissedCount: 1, DormantRounds: 1, FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID},
	}
	for i := range defects {
		if err := db.Create(&defects[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType,
		Coverage: &coverage.Coverage{PolicyVersion: "v1", PlanReconciliation: &coverage.PlanReconciliation{MissingUnits: []string{"missing"}}},
		Scope: []models.ScanScopeEntry{
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: "src/a.cpp", Outcome: ScopeScanned},
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: "src/b.cpp", Outcome: ScopeScanned},
		},
		Lifecycle: LifecyclePolicy{ResolvedRounds: 1, DormantThreshold: 1, ObsoleteAfterDormant: 2, RequireCoverage: true},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := advanceUnmatchedDefects(tx, defects, map[uint]bool{}, input, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("advanceUnmatchedDefects() error = %v", err)
	}
	var active, dormant models.Defect
	if err := db.First(&active, defects[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&dormant, defects[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if active.Status != DefectStatusCoverageGap || active.MissedCount != 0 {
		t.Fatalf("active defect advanced: %+v", active)
	}
	if dormant.Status != DefectStatusDormant || dormant.MissedCount != 1 || dormant.DormantRounds != 1 {
		t.Fatalf("dormant defect advanced: %+v", dormant)
	}
}
