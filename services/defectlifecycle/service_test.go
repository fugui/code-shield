package defectlifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"

	"code-common/backend/testdb"
)

func TestPersistScanFactsStoresRawFindingsAndScope(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle", &models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{}, &models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{})

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

	findings := []models.AnalysisFinding{
		{FilePath: "/tmp/repo/src/b.cpp", LineNumber: "20", Title: "b issue", Severity: "严重", TriggerLine: "delete p", ScopeSymbol: "Foo"},
		{FilePath: "src/a.cpp", LineNumber: "10", Title: "a issue", Severity: "致命", TriggerLine: "delete p", ScopeSymbol: "Bar"},
	}
	coverageFact := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/a.cpp", Hash: "aaaa", Status: coverage.StatusSuccess},
			{Path: "src/b.cpp", Hash: "bbbb", Status: coverage.StatusFailed, Error: "timeout"},
		},
	}
	result, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: "/tmp/repo", TaskType: taskType,
		Findings: findings, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("PersistScanFacts failed: %v", err)
	}
	if len(result.Findings) != 2 || result.Findings[0].ObservationGroupUID == "" {
		t.Fatalf("unexpected findings: %+v", result.Findings)
	}
	if result.Findings[0].AnchorConfidence != AnchorHigh {
		t.Fatalf("expected HIGH anchor, got %q", result.Findings[0].AnchorConfidence)
	}
	if result.CoverageState != CoveragePartial {
		t.Fatalf("expected PARTIAL, got %q", result.CoverageState)
	}

	var stored []models.AnalysisFinding
	if err := db.Order("id").Find(&stored).Error; err != nil {
		t.Fatalf("load findings: %v", err)
	}
	if len(stored) != 2 || stored[0].FilePath != "src/a.cpp" {
		t.Fatalf("findings not normalized and ordered: %+v", stored)
	}
	var scopes []models.ScanScopeEntry
	if err := db.Order("norm_path").Find(&scopes).Error; err != nil {
		t.Fatalf("load scope: %v", err)
	}
	if len(scopes) != 2 || scopes[0].Outcome != ScopeScanned || scopes[1].Outcome != ScopeFailed {
		t.Fatalf("unexpected scope entries: %+v", scopes)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if updated.CoverageState != CoveragePartial || !updated.CoverageDegraded || updated.WorktreeClean {
		t.Fatalf("coverage metadata not persisted: %+v", updated)
	}

	findings[0].Title = "renamed issue"
	if _, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: "/tmp/repo", TaskType: taskType,
		Findings: findings, Coverage: coverageFact,
	}); err != nil {
		t.Fatalf("retry PersistScanFacts failed: %v", err)
	}
	var count int64
	if err := db.Model(&models.AnalysisFinding{}).Where("task_report_id = ?", report.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("expected idempotent finding replace, got %d err=%v", count, err)
	}
}

func TestPersistScanFactsRetriesSameReportLedger(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "int calculate(int value) {\n    delete value;\n    return value;\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "a.cpp"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_same_report_retry",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.User{}, &models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "retry-demo", URL: "https://example.com/retry-demo.git"}
	taskType := models.TaskType{Name: "coredump-retry", DisplayName: "Core Dump Retry"}
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
	input := ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{{
			FilePath: "src/a.cpp", LineNumber: "2", Title: "invalid delete", Severity: "严重",
			TriggerLine: "delete value", ScopeSymbol: "calculate",
		}},
		Coverage: &coverage.Coverage{
			PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
			Files: []coverage.File{{Path: "src/a.cpp", Hash: "aaaa", Status: coverage.StatusSuccess}},
		},
	}
	if _, err := PersistScanFacts(input); err != nil {
		t.Fatalf("first ledger commit: %v", err)
	}
	if _, err := PersistScanFacts(input); err != nil {
		t.Fatalf("same report ledger retry: %v", err)
	}
	var observationCount, defectCount int64
	if err := db.Model(&models.DefectObservation{}).Where("report_id = ?", report.ID).Count(&observationCount).Error; err != nil || observationCount != 1 {
		t.Fatalf("expected one retry observation, got %d err=%v", observationCount, err)
	}
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("expected one retry defect, got %d err=%v", defectCount, err)
	}
}

func TestPersistScanFactsCommitsLedgerLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "int calculate(int value) {\n    delete value;\n    return value;\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "a.cpp"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_ledger",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.User{}, &models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "demo", URL: "https://example.com/demo.git"}
	taskType := models.TaskType{Name: "cpp-review", DisplayName: "C++ Review"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	finding := models.AnalysisFinding{
		FilePath: "src/a.cpp", LineNumber: "2", Title: "invalid delete", Severity: "严重",
		TriggerLine: "delete value", ScopeSymbol: "calculate",
	}
	coverageFact := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/a.cpp", Hash: "aaaa", Status: coverage.StatusSuccess}},
	}
	firstReport := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&firstReport).Error; err != nil {
		t.Fatal(err)
	}
	first, err := PersistScanFacts(ScanInput{
		DB: db, Report: firstReport, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{finding}, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if !first.LedgerReady || len(first.Observations) != 1 || first.Observations[0].Verdict != VerdictNew {
		t.Fatalf("unexpected first ledger result: %+v", first)
	}
	if len(first.NewDefectIDs) != 1 {
		t.Fatalf("expected one new defect, got %+v", first.NewDefectIDs)
	}
	defectID := first.NewDefectIDs[0]

	secondReport := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&secondReport).Error; err != nil {
		t.Fatal(err)
	}
	second, err := PersistScanFacts(ScanInput{
		DB: db, Report: secondReport, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{finding}, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second.Observations) != 1 || second.Observations[0].Verdict != VerdictExisted {
		t.Fatalf("unexpected second ledger result: %+v", second)
	}
	if second.Observations[0].DefectID == nil || *second.Observations[0].DefectID != defectID {
		t.Fatalf("expected defect %d, got %+v", defectID, second.Observations[0])
	}
	if len(second.NewDefectIDs) != 0 {
		t.Fatalf("second scan should not create defects: %+v", second.NewDefectIDs)
	}

	if _, err := PersistScanFacts(ScanInput{
		DB: db, Report: secondReport, Repo: repo, RepoRoot: root, TaskType: taskType,
		Findings: []models.AnalysisFinding{finding}, Coverage: coverageFact,
	}); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	var observationCount, defectCount int64
	if err := db.Model(&models.DefectObservation{}).Where("report_id = ?", secondReport.ID).Count(&observationCount).Error; err != nil || observationCount != 1 {
		t.Fatalf("expected one retry observation, got %d err=%v", observationCount, err)
	}
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("expected one defect, got %d err=%v", defectCount, err)
	}
	var retriedDefect models.Defect
	if err := db.First(&retriedDefect, defectID).Error; err != nil {
		t.Fatalf("load retried defect: %v", err)
	}
	if retriedDefect.MissedCount != 0 || retriedDefect.DormantRounds != 0 || retriedDefect.RowVersion != 1 {
		t.Fatalf("retry mutated lifecycle state: %+v", retriedDefect)
	}

	assignee := models.User{Username: "assignee", Email: "assignee@example.com"}
	if err := db.Create(&assignee).Error; err != nil {
		t.Fatalf("create assignee: %v", err)
	}
	if err := AssignDefect(db, defectID, &assignee.ID, assignee.ID); err != nil {
		t.Fatalf("assign defect: %v", err)
	}
	workbench, err := ListMyDefects(db, WorkbenchQuery{UserID: assignee.ID})
	if err != nil {
		t.Fatalf("list my defects: %v", err)
	}
	if workbench.Total != 1 || len(workbench.Items) != 1 {
		t.Fatalf("unexpected workbench page: %+v", workbench)
	}
	item := workbench.Items[0]
	if item.ID != defectID || item.AssigneeID == nil || *item.AssigneeID != assignee.ID {
		t.Fatalf("unexpected assignment projection: %+v", item)
	}
	if item.Title != finding.Title || item.FilePath != "src/a.cpp" || len(item.StatusLog) == 0 {
		t.Fatalf("expected finding payload and events without duplication: %+v", item)
	}
}
