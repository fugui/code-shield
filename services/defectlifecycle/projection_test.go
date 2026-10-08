package defectlifecycle

import (
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestGetReportReconciliationProjectsUnmatchedOpenDefects(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_projection",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.Defect{},
		&models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{}, &models.AnalysisFinding{},
	)

	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}

	previousReport := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&previousReport).Error; err != nil {
		t.Fatalf("create previous report: %v", err)
	}
	committedAt := time.Now()
	report := models.TaskReport{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess,
		LedgerCommittedAt: &committedAt,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	unmatchedActive := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		StatusReason: "changed file not reproduced", CanonicalFingerprint: "fingerprint-active",
		IdentityKind: "K1", NormPath: "src/active.cpp", Severity: "严重",
		FirstReportID: previousReport.ID, LastSeenReportID: previousReport.ID,
		LastMatchedReportID: previousReport.ID, MissedCount: 1,
	}
	unmatchedDormant := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusDormant,
		StatusReason: "low-risk defect not reproduced", CanonicalFingerprint: "fingerprint-dormant",
		IdentityKind: "K1", NormPath: "src/dormant.cpp", Severity: "提示",
		FirstReportID: previousReport.ID, LastSeenReportID: previousReport.ID,
		LastMatchedReportID: previousReport.ID, MissedCount: 2, DormantRounds: 1,
	}
	matchedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-matched", IdentityKind: "K1",
		NormPath: "src/matched.cpp", Severity: "严重", FirstReportID: previousReport.ID,
		LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
	}
	resolvedDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusResolved,
		CanonicalFingerprint: "fingerprint-resolved", IdentityKind: "K1",
		NormPath: "src/resolved.cpp", Severity: "严重", FirstReportID: previousReport.ID,
		LastSeenReportID: previousReport.ID, LastMatchedReportID: previousReport.ID,
	}
	for _, defect := range []*models.Defect{&unmatchedActive, &unmatchedDormant, &matchedDefect, &resolvedDefect} {
		if err := db.Create(defect).Error; err != nil {
			t.Fatalf("create defect %s: %v", defect.CanonicalFingerprint, err)
		}
	}

	observation := models.DefectObservation{
		ReportID:            previousReport.ID,
		RepoID:              repo.ID,
		TaskTypeID:          taskType.ID,
		ObservationGroupUID: "group-active",
		DefectID:            &unmatchedActive.ID,
		Verdict:             VerdictExisted,
	}
	if err := db.Create(&observation).Error; err != nil {
		t.Fatalf("create observation: %v", err)
	}
	finding := models.AnalysisFinding{
		TaskReportID: previousReport.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		Severity: "严重", Title: "invalid delete in active path",
		Detail:   "A pointer is deleted and later dereferenced.",
		FilePath: "src/active.cpp", LineNumber: "12", ObservationGroupUID: "group-active",
	}
	if err := db.Create(&finding).Error; err != nil {
		t.Fatalf("create finding: %v", err)
	}

	result, err := GetReportReconciliation(db, report.ID)
	if err != nil {
		t.Fatalf("GetReportReconciliation failed: %v", err)
	}
	if len(result.UnmatchedOpenDefects) != 2 {
		t.Fatalf("expected 2 unmatched open defects, got %d: %+v", len(result.UnmatchedOpenDefects), result.UnmatchedOpenDefects)
	}
	ids := map[uint]bool{}
	for _, defect := range result.UnmatchedOpenDefects {
		ids[defect.ID] = true
	}
	if !ids[unmatchedActive.ID] || !ids[unmatchedDormant.ID] {
		t.Fatalf("expected unmatched open defects in projection: %+v", result.UnmatchedOpenDefects)
	}
	for _, defect := range result.UnmatchedOpenDefects {
		if defect.ID == unmatchedActive.ID && defect.Title != finding.Title {
			t.Fatalf("expected enriched title %q, got %+v", finding.Title, defect)
		}
	}
	if ids[matchedDefect.ID] || ids[resolvedDefect.ID] {
		t.Fatalf("matched or closed defects should not be projected: %+v", result.UnmatchedOpenDefects)
	}
}
