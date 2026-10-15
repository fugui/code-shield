package defectlifecycle

import (
	"testing"
	"time"

	"code-shield/models"
)

func TestMutateProbableRebindsAlreadyBoundObservation(t *testing.T) {
	db := setupTestDB(t)

	repo := models.Repository{Name: "gov-demo", URL: "https://example.com/gov-demo.git"}
	taskType := models.TaskType{Name: "gov-type", DisplayName: "Governance Task Type"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}

	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	oldDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusHumanClosed,
		CanonicalFingerprint: "fingerprint-old", IdentityKind: "K1",
		NormPath: "src/test.cpp", Severity: "严重",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	newTargetDefect := models.Defect{
		RepoID: repo.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fingerprint-new", IdentityKind: "K1",
		NormPath: "src/test.cpp", Severity: "严重",
		FirstReportID: report.ID, LastSeenReportID: report.ID, LastMatchedReportID: report.ID,
		RowVersion: 1,
	}
	if err := db.Create(&oldDefect).Error; err != nil {
		t.Fatalf("create old defect: %v", err)
	}
	if err := db.Create(&newTargetDefect).Error; err != nil {
		t.Fatalf("create new target defect: %v", err)
	}

	finding := models.AnalysisFinding{
		TaskReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		FilePath: "src/test.cpp", LineNumber: "10", Title: "possible leak",
		Severity: "严重", Category: "resource",
	}
	groups := BuildObservationGroups("", report.ID, []models.AnalysisFinding{finding})
	if len(groups) == 0 {
		t.Fatalf("expected at least one observation group")
	}
	groupUID := groups[0].UID
	finding.ObservationGroupUID = groupUID
	if err := db.Create(&finding).Error; err != nil {
		t.Fatalf("create finding: %v", err)
	}

	// 初始为已绑定到 oldDefect 的 PROBABLE 观测
	obs := models.DefectObservation{
		ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
		ObservationGroupUID: groupUID,
		DefectID:            &oldDefect.ID,
		Verdict:             VerdictProbable,
		Confidence:          0.6,
		CreatedAt:           time.Now(),
	}
	if err := db.Create(&obs).Error; err != nil {
		t.Fatalf("create observation: %v", err)
	}

	// 执行重绑到 newTargetDefect
	reboundDefect, err := BindProbable(db, report.ID, groupUID, newTargetDefect.ID, GovernanceInput{
		Reason:  "rebind to correct defect",
		ActorID: 1,
	})
	if err != nil {
		t.Fatalf("mutate probable rebind failed: %v", err)
	}

	if reboundDefect == nil || reboundDefect.ID != newTargetDefect.ID {
		t.Fatalf("expected rebound defect ID %d, got %+v", newTargetDefect.ID, reboundDefect)
	}

	// 验证 observation 记录已更新
	var updatedObs models.DefectObservation
	if err := db.Where("report_id = ? AND observation_group_uid = ?", report.ID, groupUID).First(&updatedObs).Error; err != nil {
		t.Fatalf("load updated observation: %v", err)
	}
	if updatedObs.DefectID == nil || *updatedObs.DefectID != newTargetDefect.ID {
		t.Fatalf("expected observation DefectID to be %d, got %v", newTargetDefect.ID, updatedObs.DefectID)
	}
	if updatedObs.Verdict != VerdictExisted || updatedObs.MatchTier != MatchHuman {
		t.Fatalf("expected VerdictExisted and MatchHuman, got verdict=%s match_tier=%s", updatedObs.Verdict, updatedObs.MatchTier)
	}
}
