package defectlifecycle

import (
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"

	"code-common/backend/testdb"
)

func TestLedgerCampaignProjectionAndWorkflow(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_campaign",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.User{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{}, &models.Defect{},
		&models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "campaign-demo", URL: "https://example.com/campaign-demo.git", Branch: "release/1.2"}
	taskType := models.TaskType{Name: "ledger-campaign", DisplayName: "Ledger Campaign", IsCampaign: true, GovernanceMode: models.GovernanceModeFullLedger}
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
	finding := models.AnalysisFinding{
		FilePath: "src/a.cpp", LineNumber: "2", Title: "invalid delete", Detail: "detail",
		Severity: "严重", Category: "memory", Suggestion: "use smart pointer",
		TriggerLine: "delete value", ScopeSymbol: "calculate",
		HunterClaim: "hunter claim", ChallengerArg: "challenger argument", JudgeVerdict: "judge verdict",
	}
	coverageFact := &coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{{Path: "src/a.cpp", Hash: "aaaa", Status: coverage.StatusSuccess}},
	}
	result, err := PersistScanFacts(ScanInput{
		DB: db, Report: report, Repo: repo, RepoRoot: t.TempDir(), TaskType: taskType,
		Findings: []models.AnalysisFinding{finding}, Coverage: coverageFact,
	})
	if err != nil {
		t.Fatalf("persist scan facts: %v", err)
	}
	defectID := result.NewDefectIDs[0]

	page, err := ListCampaignDefects(db, CampaignQuery{TaskTypeID: taskType.ID, RepoID: repo.ID})
	if err != nil {
		t.Fatalf("list campaign defects: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected campaign page: %+v", page)
	}
	item := page.Items[0]
	if item.ID != defectID || item.Title != finding.Title || item.Category != finding.Category || item.Status != "open" {
		t.Fatalf("unexpected campaign projection: %+v", item)
	}
	if item.HunterClaim != finding.HunterClaim || item.ChallengerArg != finding.ChallengerArg ||
		item.JudgeVerdict != finding.JudgeVerdict || item.TriggerLine != finding.TriggerLine ||
		item.ScopeSymbol != finding.ScopeSymbol {
		t.Fatalf("unexpected debate projection: %+v", item)
	}
	if item.RepoURL != repo.URL || item.RepoBranch != repo.Branch {
		t.Fatalf("unexpected repository projection: url=%q branch=%q", item.RepoURL, item.RepoBranch)
	}

	assignee := models.User{Name: "Alice", Email: "alice@example.com"}
	if err := db.Create(&assignee).Error; err != nil {
		t.Fatalf("create assignee: %v", err)
	}
	if _, err := UpdateDefectWorkflow(db, defectID, DefectWorkflowInput{
		AssigneeID: &assignee.ID, AssigneeSet: true, ActorID: assignee.ID,
	}); err != nil {
		t.Fatalf("assign without status change: %v", err)
	}
	assigned, err := GetCampaignDefect(db, taskType.ID, defectID)
	if err != nil {
		t.Fatalf("load assigned defect: %v", err)
	}
	if len(assigned.StatusLog) == 0 || assigned.StatusLog[0]["user"] != assignee.Name {
		t.Fatalf("expected actor name in status log, got %+v", assigned.StatusLog)
	}
	if assigned.HunterClaim != finding.HunterClaim || assigned.JudgeVerdict != finding.JudgeVerdict {
		t.Fatalf("expected debate evidence in single defect projection: %+v", assigned)
	}
	updated, err := UpdateDefectWorkflow(db, defectID, DefectWorkflowInput{
		Status: "resolved", Feedback: "fixed", AssigneeID: &assignee.ID,
		AssigneeSet: true, ActorID: assignee.ID,
	})
	if err != nil {
		t.Fatalf("update workflow: %v", err)
	}
	if updated.Status != "resolved" || updated.AssigneeID == nil || *updated.AssigneeID != assignee.ID {
		t.Fatalf("unexpected updated finding: %+v", updated)
	}
	var defect models.Defect
	if err := db.First(&defect, defectID).Error; err != nil {
		t.Fatalf("load defect: %v", err)
	}
	if defect.Status != DefectStatusResolved || !defect.HumanLocked || defect.StatusReason != "fixed" {
		t.Fatalf("unexpected defect workflow state: %+v", defect)
	}

	summaries, err := ListLedgerRepoSummaries(db, taskType.ID)
	if err != nil {
		t.Fatalf("list repo summaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].OpenIssues != 0 || summaries[0].ResolvedIssues != 1 {
		t.Fatalf("unexpected repo summaries: %+v", summaries)
	}
	trend, err := LedgerTrend(db, taskType.ID, []uint{repo.ID})
	if err != nil {
		t.Fatalf("ledger trend: %v", err)
	}
	if len(trend) != 30 || trend[29].ResolvedIssues != 1 || trend[29].FixRate != 100 {
		t.Fatalf("unexpected trend: %+v", trend[29])
	}
}

func TestLedgerCampaignStatsRemainBaselineWhenFiltered(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_campaign_stats",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.User{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{}, &models.Defect{},
		&models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repo := models.Repository{Name: "campaign-stats-demo", URL: "https://example.com/campaign-stats-demo.git"}
	taskType := models.TaskType{
		Name: "ledger-campaign-stats", DisplayName: "Ledger Campaign Stats",
		IsCampaign: true, GovernanceMode: models.GovernanceModeFullLedger,
	}
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

	findingSeeds := []models.AnalysisFinding{
		{Severity: "严重", Category: "memory", Title: "memory defect", FilePath: "src/memory.cpp", ObservationGroupUID: "stats-memory"},
		{Severity: "一般", Category: "concurrency", Title: "race defect", FilePath: "src/race.cpp", ObservationGroupUID: "stats-race"},
	}
	defectSeeds := []models.Defect{
		{Status: DefectStatusActive, CanonicalFingerprint: "stats-memory", IdentityKind: "EXACT",
			NormPath: "src/memory.cpp", ScopeKey: "memory", DefectClassMajor: "memory",
			Severity: "严重", FirstReportID: report.ID, LastSeenReportID: report.ID},
		{Status: DefectStatusVerifiedPending, CanonicalFingerprint: "stats-race", IdentityKind: "EXACT",
			NormPath: "src/race.cpp", ScopeKey: "race", DefectClassMajor: "concurrency",
			Severity: "一般", FirstReportID: report.ID, LastSeenReportID: report.ID},
	}
	defectIDs := make([]uint, 0, len(defectSeeds))
	for index := range defectSeeds {
		defect := &defectSeeds[index]
		defect.RepoID = repo.ID
		defect.TaskTypeID = taskType.ID
		if err := db.Create(defect).Error; err != nil {
			t.Fatalf("create defect %d: %v", index, err)
		}
		defectIDs = append(defectIDs, defect.ID)

		finding := &findingSeeds[index]
		finding.TaskReportID = report.ID
		finding.TaskTypeID = taskType.ID
		finding.RepoID = repo.ID
		if err := db.Create(finding).Error; err != nil {
			t.Fatalf("create finding %d: %v", index, err)
		}
		observation := models.DefectObservation{
			ReportID: report.ID, RepoID: repo.ID, TaskTypeID: taskType.ID,
			ObservationGroupUID: finding.ObservationGroupUID, DefectID: &defect.ID,
			Verdict: "CONFIRMED", MatchTier: "EXACT",
		}
		if err := db.Create(&observation).Error; err != nil {
			t.Fatalf("create observation %d: %v", index, err)
		}
	}

	page, err := ListCampaignDefects(db, CampaignQuery{
		TaskTypeID: taskType.ID, RepoID: repo.ID, Status: "open", Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list filtered campaign defects: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != defectIDs[0] {
		t.Fatalf("unexpected filtered campaign page: total=%d items=%+v", page.Total, page.Items)
	}
	if page.StatusStats["open"] != 1 || page.StatusStats["analyzing"] != 1 {
		t.Fatalf("unexpected filtered status stats: %+v", page.StatusStats)
	}
	if page.SeverityStats["严重"] != 1 || page.SeverityStats["一般"] != 1 {
		t.Fatalf("unexpected filtered severity stats: %+v", page.SeverityStats)
	}
	if page.CategoryStats["memory"] != 1 || page.CategoryStats["concurrency"] != 1 {
		t.Fatalf("unexpected filtered category stats: %+v", page.CategoryStats)
	}
	if len(page.Categories) != 2 {
		t.Fatalf("expected baseline category options, got %+v", page.Categories)
	}
}

func TestLedgerTrend_GlobalAggregatesAllRepos(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "defectlifecycle_trend_global",
		&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.User{},
		&models.AnalysisFinding{}, &models.ScanScopeEntry{}, &models.Defect{},
		&models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	repoA := models.Repository{Name: "repo-a", URL: "https://example.com/repo-a.git"}
	repoB := models.Repository{Name: "repo-b", URL: "https://example.com/repo-b.git"}
	taskType := models.TaskType{
		Name: "ledger-trend-global", DisplayName: "Ledger Trend Global",
		IsCampaign: true, GovernanceMode: models.GovernanceModeFullLedger,
	}
	if err := db.Create(&repoA).Error; err != nil {
		t.Fatalf("create repoA: %v", err)
	}
	if err := db.Create(&repoB).Error; err != nil {
		t.Fatalf("create repoB: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	reportA := models.TaskReport{RepoID: repoA.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	reportB := models.TaskReport{RepoID: repoB.ID, TaskTypeID: taskType.ID, Status: models.StatusSuccess}
	if err := db.Create(&reportA).Error; err != nil {
		t.Fatalf("create reportA: %v", err)
	}
	if err := db.Create(&reportB).Error; err != nil {
		t.Fatalf("create reportB: %v", err)
	}

	defectA := models.Defect{
		RepoID: repoA.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-a", IdentityKind: "EXACT", NormPath: "src/a.go",
		FirstReportID: reportA.ID, LastSeenReportID: reportA.ID,
	}
	defectB := models.Defect{
		RepoID: repoB.ID, TaskTypeID: taskType.ID, Status: DefectStatusActive,
		CanonicalFingerprint: "fp-b", IdentityKind: "EXACT", NormPath: "src/b.go",
		FirstReportID: reportB.ID, LastSeenReportID: reportB.ID,
	}
	defectMerged := models.Defect{
		RepoID: repoA.ID, TaskTypeID: taskType.ID, Status: DefectStatusMerged,
		CanonicalFingerprint: "fp-m", IdentityKind: "EXACT", NormPath: "src/m.go",
		FirstReportID: reportA.ID, LastSeenReportID: reportA.ID,
	}
	if err := db.Create(&defectA).Error; err != nil {
		t.Fatalf("create defectA: %v", err)
	}
	if err := db.Create(&defectB).Error; err != nil {
		t.Fatalf("create defectB: %v", err)
	}
	if err := db.Create(&defectMerged).Error; err != nil {
		t.Fatalf("create defectMerged: %v", err)
	}

	// 1. 全平台模式 (repoIDs 为空或 nil)：应聚合 repoA 和 repoB 的缺陷，且排除 DefectStatusMerged
	globalTrend, err := LedgerTrend(db, taskType.ID, nil)
	if err != nil {
		t.Fatalf("global ledger trend: %v", err)
	}
	if len(globalTrend) != 30 {
		t.Fatalf("expected 30 trend points, got %d", len(globalTrend))
	}
	if globalTrend[29].TotalIssues != 2 || globalTrend[29].OpenIssues != 2 || globalTrend[29].ResolvedIssues != 0 {
		t.Fatalf("unexpected global trend point: %+v", globalTrend[29])
	}

	// 2. 单代码仓过滤模式：仅统计 repoA (排除了 defectB 与 defectMerged)
	repoATrend, err := LedgerTrend(db, taskType.ID, []uint{repoA.ID})
	if err != nil {
		t.Fatalf("repoA ledger trend: %v", err)
	}
	if repoATrend[29].TotalIssues != 1 || repoATrend[29].OpenIssues != 1 {
		t.Fatalf("unexpected repoA trend point: %+v", repoATrend[29])
	}
}
