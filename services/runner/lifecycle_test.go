package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/planner"
	"code-shield/services/engines/profile"
)

type lifecycleTestEngine struct {
	primaryUnitIDs []string
	repoRoot       string
}

func (engine *lifecycleTestEngine) Name() string { return "debate_full" }

func (engine *lifecycleTestEngine) Run(ctx *engines.EngineContext) (*engines.EngineResult, error) {
	if ctx.ScanPlan == nil {
		return nil, fmt.Errorf("scope gate did not provide ScanPlan")
	}
	if len(ctx.PrimaryUnits) == 0 {
		return nil, fmt.Errorf("scope gate did not provide PrimaryUnits")
	}

	records := make([]coverage.AssessmentRecord, 0, len(ctx.PrimaryUnits))
	findings := make([]models.AnalysisFinding, 0, len(ctx.PrimaryUnits))
	isChangeReview := ctx.Profile.Name == profile.NameChangeReview
	for _, unit := range ctx.PrimaryUnits {
		records = append(records, coverage.AssessmentRecord{PrimaryUnitID: unit.ID, Status: "assessed"})
		if unit.Kind != coverage.PlanUnitFile && !isChangeReview {
			continue
		}
		unitPath := unit.Path
		if unitPath == "" {
			continue
		}
		findings = append(findings, models.AnalysisFinding{
			PrimaryUnitID:    unit.ID,
			FilePath:         unitPath,
			LineNumber:       "1",
			Severity:         "严重",
			Category:         "multithreading",
			Title:            "lifecycle finding",
			Detail:           "validated by lifecycle integration test",
			Suggestion:       "fix it",
			AssessmentStatus: "assessed",
		})
	}

	details := []engines.ChunkDetails{{
		ChunkName:        "lifecycle",
		Status:           "success",
		Attempts:         1,
		Files:            []string{"src/main.cpp"},
		ArtifactComplete: engines.ArtifactCompletePtr(true),
		ArtifactState:    "observed_complete",
	}}
	if isChangeReview {
		details[0].PlannedFiles = make([]coverage.PlannedFile, 0, len(ctx.PrimaryUnits))
		for _, unit := range ctx.PrimaryUnits {
			hunkRange := ""
			if unit.EndLine > unit.StartLine {
				hunkRange = fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
			} else if unit.StartLine > 0 {
				hunkRange = fmt.Sprintf("%d", unit.StartLine)
			}
			details[0].PlannedFiles = append(details[0].PlannedFiles, coverage.PlannedFile{
				Path:        unit.Path,
				DiffTouched: true,
				HunkRanges:  []string{hunkRange},
			})
		}
	}
	ctx.Coverage = engines.BuildScanCoverage(ctx, details, ctx.ScanPlan)
	reconciliation := coverage.ReconcilePlanUnits(ctx.PrimaryUnits, records)
	ctx.Coverage.PlanReconciliation = &reconciliation

	return &engines.EngineResult{
		Findings:           findings,
		SummaryChunks:      details,
		PlanReconciliation: reconciliation,
		AssessmentRecords:  records,
		HunterTokens:       10,
		Tier2Tokens:        20,
	}, nil
}

func runLifecycleGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func runLifecycleGitAt(t *testing.T, dir string, commitTime string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+commitTime,
		"GIT_COMMITTER_DATE="+commitTime,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func lifecycleExecutionSnapshot(t *testing.T, taskType models.TaskType) ([]byte, []byte, string) {
	t.Helper()
	parsedProfile, profileHash, err := profile.Parse(json.RawMessage(taskType.EngineConfig))
	if err != nil {
		t.Fatalf("parse lifecycle profile: %v", err)
	}
	scanProfileRaw, err := json.Marshal(parsedProfile)
	if err != nil {
		t.Fatalf("marshal lifecycle scan profile: %v", err)
	}
	snapshotRaw, err := json.Marshal(models.ExecutionContextSnapshot{
		EngineMode:           taskType.EngineMode,
		ScanProfile:          scanProfileRaw,
		ScanProfileHash:      profileHash,
		AssessmentConfig:     json.RawMessage(`{"version":1,"schema":"code-shield.assessment-config.v1","domain":"lifecycle"}`),
		AssessmentConfigHash: "assessment-hash",
		PromptContent:        "lifecycle prompt",
		PromptContentHash:    "prompt-hash",
		Categories:           []string{},
		CategorySchemaHash:   "category-hash",
	})
	if err != nil {
		t.Fatalf("marshal lifecycle snapshot: %v", err)
	}
	return scanProfileRaw, snapshotRaw, profileHash
}

func TestRunTaskSyncLifecyclePersistsFindingsAndReconciliation(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_lifecycle",
		&models.Department{}, &models.User{}, &models.Repository{}, &models.TaskType{},
		&models.TaskReport{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{},
		&models.TaskTriggerLog{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	dataDir := t.TempDir()
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = dataDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	sourceRepo := filepath.Join(dataDir, "lifecycle-source")
	if err := os.MkdirAll(filepath.Join(sourceRepo, "src"), 0755); err != nil {
		t.Fatalf("create source repo: %v", err)
	}
	sourceFile := filepath.Join(sourceRepo, "src", "main.cpp")
	if err := os.WriteFile(sourceFile, []byte("int main() { return 0; }\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	runLifecycleGit(t, sourceRepo, "init", "--initial-branch=master")
	runLifecycleGit(t, sourceRepo, "config", "user.email", "lifecycle@example.com")
	runLifecycleGit(t, sourceRepo, "config", "user.name", "Lifecycle Test")
	runLifecycleGit(t, sourceRepo, "add", ".")
	runLifecycleGit(t, sourceRepo, "commit", "-m", "initial")

	unique := t.Name()
	department := models.Department{Name: unique + "-department"}
	if err := db.Create(&department).Error; err != nil {
		t.Fatalf("create department: %v", err)
	}
	user := models.User{Username: unique + "-user", Email: "lifecycle@example.com"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := models.Repository{
		DepartmentID: department.ID,
		OwnerID:      user.ID,
		Name:         unique + "-repo",
		URL:          "file://" + sourceRepo,
		Branch:       "master",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	taskType := models.TaskType{
		Name:           unique + "-full-review",
		DisplayName:    "Lifecycle Full Review",
		EngineMode:     "debate_full",
		EngineConfig:   []byte(`{"scan_profile":{"version":1,"name":"full_review"}}`),
		TargetScope:    "business",
		GovernanceMode: models.GovernanceModeFullLedger,
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	scanProfileRaw, snapshotRaw, profileHash := lifecycleExecutionSnapshot(t, taskType)
	report := models.TaskReport{
		RepoID:                 repo.ID,
		TaskTypeID:             taskType.ID,
		Status:                 models.StatusQueued,
		EngineMode:             taskType.EngineMode,
		ScanProfile:            scanProfileRaw,
		ScanProfileHash:        profileHash,
		PromptContentHash:      "prompt-hash",
		ExecutionSnapshot:      snapshotRaw,
		ExecutionSnapshotState: "complete",
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	oldEngine := engines.GetEngine("debate_full")
	engines.RegisterEngine("debate_full", &lifecycleTestEngine{repoRoot: sourceRepo})
	t.Cleanup(func() { engines.RegisterEngine("debate_full", oldEngine) })

	if err := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{}); err != nil {
		t.Fatalf("RunTaskSync() error = %v", err)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load updated report: %v", err)
	}
	if updated.Status != models.StatusSuccess {
		t.Fatalf("status = %q, want %q", updated.Status, models.StatusSuccess)
	}
	if !updated.CoverageComplete || updated.CoverageDegraded {
		t.Fatalf("coverage complete=%t degraded=%t", updated.CoverageComplete, updated.CoverageDegraded)
	}

	var finding models.AnalysisFinding
	if err := db.Where("task_report_id = ?", report.ID).First(&finding).Error; err != nil {
		t.Fatalf("load persisted finding: %v", err)
	}
	if finding.FilePath != "src/main.cpp" || finding.PrimaryUnitID != "src/main.cpp" {
		t.Fatalf("unexpected finding identity: path=%q unit=%q", finding.FilePath, finding.PrimaryUnitID)
	}

	var scopeCount, observationCount, defectCount int64
	if err := db.Model(&models.ScanScopeEntry{}).Where("report_id = ?", report.ID).Count(&scopeCount).Error; err != nil || scopeCount != 1 {
		t.Fatalf("scope entries = %d err=%v, want 1", scopeCount, err)
	}
	if err := db.Model(&models.DefectObservation{}).Where("report_id = ?", report.ID).Count(&observationCount).Error; err != nil || observationCount != 1 {
		t.Fatalf("ledger observations = %d err=%v, want 1", observationCount, err)
	}
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("ledger defects = %d err=%v, want 1", defectCount, err)
	}

	summaryPath := updated.GetAbsReportPath()
	summaryData, err := os.ReadFile(filepath.Join(filepath.Dir(summaryPath), fmt.Sprintf("report-%d-summary-%s.json", report.ID, repo.Name)))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var summary TaskSummaryReport
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.ScopeDecision == nil || summary.ScopeDecision.Reason != planner.ReasonPrimaryScopeReady ||
		summary.ScopeDecision.ScopeProfile != profile.NameFullReview {
		t.Fatalf("unexpected scope decision: %+v", summary.ScopeDecision)
	}
	if summary.PlanReconciliation == nil || summary.PlanReconciliation.PlannedUnits != 1 ||
		summary.PlanReconciliation.MatchedUnits != 1 ||
		len(summary.PlanReconciliation.MissingUnits) != 0 ||
		len(summary.PlanReconciliation.UnmatchedUnits) != 0 {
		t.Fatalf("unexpected plan reconciliation: %+v", summary.PlanReconciliation)
	}
}

func TestRunTaskSyncChangeReviewLifecycle(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_change_lifecycle",
		&models.Department{}, &models.User{}, &models.Repository{}, &models.TaskType{},
		&models.TaskReport{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{},
		&models.TaskTriggerLog{}, &models.AnalysisFinding{}, &models.ScanScopeEntry{},
		&models.Defect{}, &models.DefectAlias{}, &models.DefectObservation{}, &models.DefectEvent{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	dataDir := t.TempDir()
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = dataDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	sourceRepo := filepath.Join(dataDir, "change-lifecycle-source")
	if err := os.MkdirAll(filepath.Join(sourceRepo, "src"), 0755); err != nil {
		t.Fatalf("create source repo: %v", err)
	}
	sourceFile := filepath.Join(sourceRepo, "src", "main.cpp")
	if err := os.WriteFile(sourceFile, []byte("int main() { return 0; }\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	runLifecycleGit(t, sourceRepo, "init", "--initial-branch=master")
	runLifecycleGit(t, sourceRepo, "config", "user.email", "change-lifecycle@example.com")
	runLifecycleGit(t, sourceRepo, "config", "user.name", "Change Lifecycle Test")
	runLifecycleGit(t, sourceRepo, "add", ".")
	firstCommitTime := time.Now().AddDate(0, 0, -2).Format(time.RFC3339)
	runLifecycleGitAt(t, sourceRepo, firstCommitTime, "commit", "-m", "initial")
	baseCommitBytes, err := exec.Command("git", "-C", sourceRepo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve base commit: %v", err)
	}
	baseCommit := string(bytes.TrimSpace(baseCommitBytes))
	if err := os.WriteFile(sourceFile, []byte("int main() { std::exit(1); }\n"), 0644); err != nil {
		t.Fatalf("write changed source file: %v", err)
	}
	runLifecycleGit(t, sourceRepo, "add", ".")
	runLifecycleGitAt(t, sourceRepo, time.Now().Format(time.RFC3339), "commit", "-m", "change")

	unique := t.Name()
	department := models.Department{Name: unique + "-department"}
	if err := db.Create(&department).Error; err != nil {
		t.Fatalf("create department: %v", err)
	}
	user := models.User{Username: unique + "-user", Email: "change-lifecycle@example.com"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := models.Repository{
		DepartmentID: department.ID,
		OwnerID:      user.ID,
		Name:         unique + "-repo",
		URL:          "file://" + sourceRepo,
		Branch:       "master",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}
	taskType := models.TaskType{
		Name:           unique + "-change-review",
		DisplayName:    "Lifecycle Change Review",
		EngineMode:     "debate_full",
		EngineConfig:   []byte(`{"scan_profile":{"version":1,"name":"change_review","languages":["cpp","python","java"],"scope_policy":"changed_hunks","context_policy":"changed_files","base_policy":{"strategy":"since_days","since_days":1}}}`),
		GovernanceMode: models.GovernanceModeFullLedger,
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	scanProfileRaw, snapshotRaw, profileHash := lifecycleExecutionSnapshot(t, taskType)
	report := models.TaskReport{
		RepoID:                 repo.ID,
		TaskTypeID:             taskType.ID,
		Status:                 models.StatusQueued,
		EngineMode:             taskType.EngineMode,
		ScanProfile:            scanProfileRaw,
		ScanProfileHash:        profileHash,
		PromptContentHash:      "prompt-hash",
		ExecutionSnapshot:      snapshotRaw,
		ExecutionSnapshotState: "complete",
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	oldEngine := engines.GetEngine("debate_full")
	engines.RegisterEngine("debate_full", &lifecycleTestEngine{})
	t.Cleanup(func() { engines.RegisterEngine("debate_full", oldEngine) })

	if err := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{}); err != nil {
		t.Fatalf("RunTaskSync() error = %v", err)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load updated report: %v", err)
	}
	if updated.Status != models.StatusSuccess {
		t.Fatalf("status = %q, want %q", updated.Status, models.StatusSuccess)
	}
	if updated.BaseCommit != baseCommit || updated.DiffManifestHash == "" || updated.BaseCommitTime == nil {
		t.Fatalf("unexpected change baseline: base=%q hash=%q time=%v", updated.BaseCommit, updated.DiffManifestHash, updated.BaseCommitTime)
	}

	var finding models.AnalysisFinding
	if err := db.Where("task_report_id = ?", report.ID).First(&finding).Error; err != nil {
		t.Fatalf("load persisted finding: %v", err)
	}
	if finding.FilePath != "src/main.cpp" || finding.PrimaryUnitID != "src/main.cpp#h1" {
		t.Fatalf("unexpected change finding identity: path=%q unit=%q", finding.FilePath, finding.PrimaryUnitID)
	}
	var observationCount, defectCount int64
	if err := db.Model(&models.DefectObservation{}).Where("report_id = ?", report.ID).Count(&observationCount).Error; err != nil || observationCount != 1 {
		t.Fatalf("ledger observations = %d err=%v, want 1", observationCount, err)
	}
	if err := db.Model(&models.Defect{}).Where("repo_id = ?", repo.ID).Count(&defectCount).Error; err != nil || defectCount != 1 {
		t.Fatalf("ledger defects = %d err=%v, want 1", defectCount, err)
	}

	summaryPath := updated.GetAbsReportPath()
	summaryData, err := os.ReadFile(filepath.Join(filepath.Dir(summaryPath), fmt.Sprintf("report-%d-summary-%s.json", report.ID, repo.Name)))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var summary TaskSummaryReport
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.ScopeDecision == nil || summary.ScopeDecision.ScopeProfile != profile.NameChangeReview ||
		summary.ScopeDecision.PrimaryUnit != "change_hunk" || summary.ScopeDecision.ChangeHunks != 1 {
		t.Fatalf("unexpected change scope decision: %+v", summary.ScopeDecision)
	}
	if summary.PlanReconciliation == nil || summary.PlanReconciliation.PlannedUnits != 1 ||
		summary.PlanReconciliation.MatchedUnits != 1 ||
		len(summary.PlanReconciliation.MissingUnits) != 0 ||
		len(summary.PlanReconciliation.UnmatchedUnits) != 0 {
		t.Fatalf("unexpected change plan reconciliation: %+v", summary.PlanReconciliation)
	}
}
