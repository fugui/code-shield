package defectlifecycle

import (
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"gorm.io/gorm"
)

func TestAdvanceUnmatchedDefectsCountsScannedRoundWithoutReproduction(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_advance_unmatched",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	scannedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-scanned", IdentityKind: "K1",
		NormPath: "src/unchanged.cpp", Severity: "建议", BlobHash: "same-hash",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	uncoveredDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-uncovered", IdentityKind: "K1",
		NormPath: "src/failed.cpp", Severity: "建议", BlobHash: "old-hash",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	for _, defect := range []*models.Defect{&scannedDefect, &uncoveredDefect} {
		if err := db.Create(defect).Error; err != nil {
			t.Fatalf("create defect %s: %v", defect.CanonicalFingerprint, err)
		}
	}

	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, CoverageState: CoverageChangeFocus,
		Scope: []models.ScanScopeEntry{
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: scannedDefect.NormPath, BlobHash: "same-hash", Outcome: ScopeScanned},
			{ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: uncoveredDefect.NormPath, BlobHash: "new-hash", Outcome: ScopeFailed},
		},
		Lifecycle: LifecyclePolicy{
			HighRiskSeverities: []string{"致命", "严重"}, ResolvedRounds: 2,
			DormantThreshold: 2, ObsoleteAfterDormant: 8, RequireCoverage: true, RequireChange: true,
		},
	}
	defects := []models.Defect{scannedDefect, uncoveredDefect}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := advanceUnmatchedDefects(tx, defects, map[uint]bool{}, input, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("advance unmatched defects: %v", err)
	}

	var updatedScanned, updatedUncovered models.Defect
	if err := db.First(&updatedScanned, scannedDefect.ID).Error; err != nil {
		t.Fatalf("load scanned defect: %v", err)
	}
	if err := db.First(&updatedUncovered, uncoveredDefect.ID).Error; err != nil {
		t.Fatalf("load uncovered defect: %v", err)
	}
	if updatedScanned.MissedCount != 1 || updatedScanned.Status != DefectStatusCoverageGap {
		t.Fatalf("expected scanned defect to advance, got missed=%d status=%s", updatedScanned.MissedCount, updatedScanned.Status)
	}
	if updatedUncovered.MissedCount != 0 || updatedUncovered.Status != DefectStatusActive {
		t.Fatalf("expected uncovered defect to stay unchanged, got missed=%d status=%s", updatedUncovered.MissedCount, updatedUncovered.Status)
	}
}

func TestCommitLedgerAssignsSameFingerprintObservationsOnce(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_duplicate_identity",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	identity := Identity{
		RepoID: repo.ID, TaskTypeID: taskType.ID, NormPath: "src/a.cpp", ScopeKey: "scope",
		StmtShape: "shape", OccurrenceIndex: 1, DefectClassMajor: "memory_leak",
		LineStart: 158, LineEnd: 180, K2: "strong-key",
	}
	groups := []ObservationGroup{
		{UID: "group-1", Identity: identity},
		{UID: "group-2", Identity: identity},
	}
	decisions := []ObservationDecision{
		{ObservationGroupUID: groups[0].UID, Identity: identity, Verdict: VerdictNew},
		{ObservationGroupUID: groups[1].UID, Identity: identity, Verdict: VerdictNew},
	}
	input := LedgerInput{
		Report: report, Repo: repo, TaskType: taskType, Observations: groups, Decisions: decisions,
	}

	var result *LedgerResult
	err := db.Transaction(func(tx *gorm.DB) (txErr error) {
		result, txErr = commitLedger(tx, input)
		return txErr
	})
	if err != nil {
		t.Fatalf("commit ledger: %v", err)
	}
	if len(result.NewDefectIDs) != 1 {
		t.Fatalf("expected one new defect ID, got %+v", result.NewDefectIDs)
	}
	if len(result.Observations) != 2 || result.Observations[0].DefectID == nil || *result.Observations[0].DefectID != result.NewDefectIDs[0] {
		t.Fatalf("expected first observation bound to new defect, got %+v", result.Observations)
	}
	if result.Observations[1].DefectID == nil || *result.Observations[1].DefectID != result.NewDefectIDs[0] {
		t.Fatalf("expected same-fingerprint observations to share defect, got %+v", result.Observations)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected one event per new observation, got %+v", result.Events)
	}
	for _, event := range result.Events {
		if event.DefectID != result.NewDefectIDs[0] {
			t.Fatalf("expected event defect %d, got %d", result.NewDefectIDs[0], event.DefectID)
		}
	}
	var defectCount int64
	if err := db.Model(&models.Defect{}).Count(&defectCount).Error; err != nil {
		t.Fatalf("count defects: %v", err)
	}
	if defectCount != 1 {
		t.Fatalf("expected one defect, got %d", defectCount)
	}
}
