package defectlifecycle

import (
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"

	"code-common/backend/testdb"
	"gorm.io/gorm"
)

func TestConfirmProbableCreatesDefectAndTransferAliasesOnMerge(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_governance",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{},
		&models.ScanScopeEntry{}, &models.User{}, &models.Defect{}, &models.DefectAlias{},
		&models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "governance", URL: "https://example.com/governance.git"}
	taskType := models.TaskType{Name: "governance-review", DisplayName: "Governance Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	finding := models.AnalysisFinding{
		FilePath: "src/a.cpp", LineNumber: "2", Title: "source issue", Severity: "严重",
		TriggerLine: "delete value", ScopeSymbol: "calculate",
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	groups := BuildObservationGroups("", report.ID, []models.AnalysisFinding{finding})
	if len(groups) != 1 {
		t.Fatalf("expected one observation group, got %+v", groups)
	}
	observation := models.DefectObservation{
		ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		ObservationGroupUID: groups[0].UID, Verdict: VerdictProbable, MatchTier: MatchUnclaimed,
	}
	if err := db.Create(&observation).Error; err != nil {
		t.Fatal(err)
	}

	confirmed, err := ConfirmProbable(db, report.ID, groups[0].UID, GovernanceInput{ActorID: 7, Reason: "human confirmed"})
	if err != nil {
		t.Fatalf("confirm probable: %v", err)
	}
	if confirmed.ID == 0 || observation.DefectID == nil || *observation.DefectID != confirmed.ID {
		t.Fatalf("confirmed observation not bound: defect=%+v observation=%+v", confirmed, observation)
	}
	if err := CreateHumanAlias(db, confirmed.ID, " stable-human-key ", GovernanceInput{ActorID: 7}); err != nil {
		t.Fatalf("create human alias: %v", err)
	}

	targetIdentity := groups[0].Identity
	targetIdentity.RepoID, targetIdentity.TaskTypeID = repo.ID, taskType.ID
	targetFingerprint, _ := canonicalIdentity(targetIdentity)
	target := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		StatusReason: "target", CanonicalFingerprint: targetFingerprint + "-target", IdentityKind: "K1",
		NormPath: targetIdentity.NormPath, ScopeKey: targetIdentity.ScopeKey,
		SymbolPath: targetIdentity.SymbolPath, StmtShape: targetIdentity.StmtShape,
		CleanToken: targetIdentity.CleanToken, OccurrenceIndex: targetIdentity.OccurrenceIndex,
		DefectClassMajor: targetIdentity.DefectClassMajor, Severity: targetIdentity.Severity,
		LineStart: &targetIdentity.LineStart, LineEnd: &targetIdentity.LineEnd,
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID, RowVersion: 1,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	mergedTarget, err := MergeDefects(db, confirmed.ID, target.ID, GovernanceInput{ActorID: 7, Reason: "duplicate"})
	if err != nil {
		t.Fatalf("merge defects: %v", err)
	}
	if mergedTarget.ID != target.ID {
		t.Fatalf("unexpected merge target: %+v", mergedTarget)
	}
	var aliasCount int64
	if err := db.Model(&models.DefectAlias{}).Where("defect_id = ?", target.ID).Count(&aliasCount).Error; err != nil || aliasCount == 0 {
		t.Fatalf("expected aliases transferred to target, got %d err=%v", aliasCount, err)
	}

	nextReport := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&nextReport).Error; err != nil {
		t.Fatal(err)
	}
	next, err := PersistScanFacts(ScanInput{
		DB: db, Report: nextReport, Repo: repo, TaskType: taskType,
		Findings: []models.AnalysisFinding{finding},
		Coverage: &coverage.Coverage{PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{{Path: "src/a.cpp", Hash: "aaaa", Status: coverage.StatusSuccess}}},
	})
	if err != nil {
		t.Fatalf("post-merge scan: %v", err)
	}
	if len(next.Observations) != 1 || next.Observations[0].DefectID == nil || *next.Observations[0].DefectID != target.ID {
		t.Fatalf("expected observation transferred to target: %+v", next.Observations)
	}
}

func TestBindObservationActivatesCoverageGapDefect(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_bind",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{},
		&models.ScanScopeEntry{}, &models.User{}, &models.Defect{}, &models.DefectAlias{},
		&models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "bind", URL: "https://example.com/bind.git"}
	taskType := models.TaskType{Name: "bind-review", DisplayName: "Bind Review"}
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
	observation := models.DefectObservation{
		ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		ObservationGroupUID: "obs-bind-0001", Verdict: VerdictProbable, MatchTier: MatchUnclaimed,
	}
	if err := db.Create(&observation).Error; err != nil {
		t.Fatal(err)
	}
	lineStart, lineEnd := 1, 3
	target := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusCoverageGap,
		StatusReason: "coverage gap", CanonicalFingerprint: "target-fingerprint", IdentityKind: "K1",
		NormPath: "src/a.cpp", ScopeKey: "target-scope", SymbolPath: "calculate",
		StmtShape: "use-value", DefectClassMajor: "memory", Severity: "严重",
		LineStart: &lineStart, LineEnd: &lineEnd,
		FirstReportID: report.ID, LastSeenReportID: report.ID, RowVersion: 3,
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}

	var bound *models.Defect
	if err := db.Transaction(func(tx *gorm.DB) error {
		observationCopy := observation
		result, bindErr := bindObservationToDefect(tx, target.ID, repo.ID, taskType.ID,
			&observationCopy, "human bound", 9, time.Now())
		if bindErr != nil {
			return bindErr
		}
		bound = result
		return nil
	}); err != nil {
		t.Fatalf("bind observation: %v", err)
	}
	var saved models.DefectObservation
	if err := db.First(&saved, observation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bound.ID != target.ID || bound.Status != DefectStatusActive || bound.RowVersion != 4 {
		t.Fatalf("unexpected bound defect: %+v", bound)
	}
	if saved.DefectID == nil || *saved.DefectID != target.ID || saved.Verdict != VerdictExisted || saved.MatchTier != MatchHuman {
		t.Fatalf("observation was not bound: %+v", saved)
	}
}
