package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/engines/profile"
)

func TestRunEngineConvergence(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_migration_engine_convergence",
		&models.TaskType{}, &models.TaskReport{}, &models.AnalysisFinding{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	tasksRoot := t.TempDir()
	taskDir := filepath.Join(tasksRoot, "change-review")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"name":         "change_review",
		"engine_mode":  "debate_full",
		"target_scope": "business",
		"engine_config": map[string]any{
			"since_days":    7,
			"exclude_paths": []string{"thirdparts"},
		},
		"governance_mode": models.GovernanceModeChangeFocus,
	}
	legacyRaw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyRawEngineConfig, err := json.Marshal(legacy["engine_config"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "meta.json"), legacyRaw, 0644); err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{
		Name:           "change_review",
		EngineMode:     "debate_full",
		TargetScope:    "business",
		EngineConfig:   legacyRawEngineConfig,
		GovernanceMode: models.GovernanceModeChangeFocus,
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: 1, TaskTypeID: taskType.ID, Status: models.StatusQueued}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	if err := RunEngineConvergence(db, tasksRoot); err != nil {
		t.Fatalf("RunEngineConvergence() error = %v", err)
	}

	var updatedTaskType models.TaskType
	if err := db.First(&updatedTaskType, taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedTaskType.EngineMode != "debate_full" {
		t.Fatalf("engine mode = %q, want debate_full", updatedTaskType.EngineMode)
	}
	if _, _, err := profile.Parse(json.RawMessage(updatedTaskType.EngineConfig)); err != nil {
		t.Fatalf("migrated engine config is invalid: %v", err)
	}
	parsedProfile, _, err := profile.Parse(json.RawMessage(updatedTaskType.EngineConfig))
	if err != nil {
		t.Fatalf("parse migrated profile: %v", err)
	}
	if parsedProfile.Name != profile.NameChangeReview || parsedProfile.ScopePolicy != "changed_hunks" ||
		parsedProfile.ContextPolicy != "changed_files" || parsedProfile.BasePolicy == nil ||
		parsedProfile.BasePolicy.SinceDays != 7 {
		t.Fatalf("unexpected migrated change profile: %+v", parsedProfile)
	}

	var updatedReport models.TaskReport
	if err := db.First(&updatedReport, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedReport.EngineMode == "" || updatedReport.ScanProfileHash == "" || updatedReport.EngineConfigHash == "" {
		t.Fatalf("report snapshot incomplete: %+v", updatedReport)
	}

	var diskTaskType models.TaskType
	metaBytes, err := os.ReadFile(filepath.Join(taskDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(metaBytes, &diskTaskType); err != nil {
		t.Fatal(err)
	}
	if _, _, err := profile.Parse(json.RawMessage(diskTaskType.EngineConfig)); err != nil {
		t.Fatalf("disk engine config is invalid: %v", err)
	}

	snapshotAfterFirst := map[string]any{
		"engine_mode":        updatedReport.EngineMode,
		"scan_profile_hash":  updatedReport.ScanProfileHash,
		"engine_config_hash": updatedReport.EngineConfigHash,
	}
	metaAfterFirst := string(metaBytes)
	if err := RunEngineConvergence(db, tasksRoot); err != nil {
		t.Fatalf("idempotent RunEngineConvergence() error = %v", err)
	}
	var idempotentReport models.TaskReport
	if err := db.First(&idempotentReport, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if idempotentReport.EngineMode != snapshotAfterFirst["engine_mode"] ||
		idempotentReport.ScanProfileHash != snapshotAfterFirst["scan_profile_hash"] ||
		idempotentReport.EngineConfigHash != snapshotAfterFirst["engine_config_hash"] {
		t.Fatalf("idempotent report snapshot changed: before=%+v after=%+v", snapshotAfterFirst, idempotentReport)
	}
	secondMetaBytes, err := os.ReadFile(filepath.Join(taskDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if metaAfterFirst != string(secondMetaBytes) {
		t.Fatal("idempotent disk migration changed meta bytes")
	}
}

func TestRunEngineConvergenceMigratesLegacyModeBeforeReportSnapshots(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_migration_legacy_mode",
		&models.TaskType{}, &models.TaskReport{}, &models.AnalysisFinding{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	tasksRoot := t.TempDir()
	taskDir := filepath.Join(tasksRoot, "legacy-code-review")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyRaw, err := json.Marshal(map[string]any{
		"name":         "legacy-code-review",
		"engine_mode":  "single",
		"target_scope": "business",
		"engine_config": map[string]any{
			"max_files": 8,
			"depth":     1,
		},
		"governance_mode": models.GovernanceModeFullLedger,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "meta.json"), legacyRaw, 0644); err != nil {
		t.Fatal(err)
	}
	legacyEngineConfig, err := json.Marshal(map[string]any{
		"max_files": 8,
		"depth":     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{
		Name:           "legacy-code-review",
		EngineMode:     "single",
		TargetScope:    "business",
		EngineConfig:   legacyEngineConfig,
		GovernanceMode: models.GovernanceModeFullLedger,
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	queuedReport := models.TaskReport{RepoID: 1, TaskTypeID: taskType.ID, Status: models.StatusQueued}
	runningReport := models.TaskReport{RepoID: 1, TaskTypeID: taskType.ID, Status: models.StatusRunning}
	if err := db.Create(&queuedReport).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&runningReport).Error; err != nil {
		t.Fatal(err)
	}

	if err := RunEngineConvergence(db, tasksRoot); err != nil {
		t.Fatalf("RunEngineConvergence() error = %v", err)
	}

	var updatedTaskType models.TaskType
	if err := db.First(&updatedTaskType, taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedTaskType.EngineMode != "debate_full" {
		t.Fatalf("engine mode = %q, want debate_full", updatedTaskType.EngineMode)
	}
	parsedProfile, profileHash, err := profile.Parse(json.RawMessage(updatedTaskType.EngineConfig))
	if err != nil {
		t.Fatalf("migrated profile is invalid: %v", err)
	}
	if parsedProfile.Name != profile.NameFullReview || profileHash == "" {
		t.Fatalf("unexpected migrated profile: name=%q hash=%q", parsedProfile.Name, profileHash)
	}

	for _, reportID := range []uint{queuedReport.ID, runningReport.ID} {
		var report models.TaskReport
		if err := db.First(&report, reportID).Error; err != nil {
			t.Fatal(err)
		}
		if report.EngineMode != "debate_full" || report.ScanProfileHash == "" || report.EngineConfigHash == "" {
			t.Fatalf("report %d snapshot incomplete: %+v", report.ID, report)
		}
	}

	firstReports := []models.TaskReport{queuedReport, runningReport}
	if err := db.Find(&firstReports, "task_type_id = ?", taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunEngineConvergence(db, tasksRoot); err != nil {
		t.Fatalf("idempotent RunEngineConvergence() error = %v", err)
	}
	secondReports := []models.TaskReport{}
	if err := db.Find(&secondReports, "task_type_id = ?", taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	for index, report := range secondReports {
		if report.EngineMode != firstReports[index].EngineMode ||
			report.ScanProfileHash != firstReports[index].ScanProfileHash ||
			report.EngineConfigHash != firstReports[index].EngineConfigHash {
			t.Fatalf("idempotent report snapshot changed: before=%+v after=%+v", firstReports[index], report)
		}
	}
}

func TestRunEngineConvergenceMigratesDatabaseOnlyLegacyTask(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_migration_database_only_legacy",
		&models.TaskType{}, &models.TaskReport{}, &models.AnalysisFinding{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	tasksRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tasksRoot, "executor-algorithm-task"), 0755); err != nil {
		t.Fatal(err)
	}
	legacyEngineConfig, err := json.Marshal(map[string]any{
		"max_files": 8,
		"depth":     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{
		Name:           "executor_algorithm_task",
		EngineMode:     "chunked",
		TargetScope:    "business",
		EngineConfig:   legacyEngineConfig,
		GovernanceMode: models.GovernanceModeFullLedger,
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: 1, TaskTypeID: taskType.ID, Status: models.StatusQueued}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}

	if err := RunEngineConvergence(db, tasksRoot); err != nil {
		t.Fatalf("RunEngineConvergence() error = %v", err)
	}

	var updatedTaskType models.TaskType
	if err := db.First(&updatedTaskType, taskType.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedTaskType.EngineMode != "debate_full" {
		t.Fatalf("engine mode = %q, want debate_full", updatedTaskType.EngineMode)
	}
	parsedProfile, profileHash, err := profile.Parse(json.RawMessage(updatedTaskType.EngineConfig))
	if err != nil {
		t.Fatalf("migrated profile is invalid: %v", err)
	}
	if parsedProfile.Name != profile.NameFullReview || parsedProfile.MaxFiles != 8 || profileHash == "" {
		t.Fatalf("unexpected migrated profile: %+v hash=%q", parsedProfile, profileHash)
	}
	var updatedReport models.TaskReport
	if err := db.First(&updatedReport, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedReport.EngineMode != "debate_full" || updatedReport.ScanProfileHash == "" || updatedReport.EngineConfigHash == "" {
		t.Fatalf("report snapshot incomplete: %+v", updatedReport)
	}
}
