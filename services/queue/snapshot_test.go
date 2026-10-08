package queue

import (
	"encoding/json"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/engines/profile"
)

func TestEnqueueTaskCreatesReportExecutionSnapshot(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_report_snapshot",
		&models.TaskType{}, &models.TaskTypeRevision{}, &models.Repository{}, &models.TaskReport{}, &models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	t.Chdir("../..")
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "snapshot-repo-" + time.Now().Format("150405.000000000"), URL: "http://example.com/snapshot.git"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{
		Name:         "deep_review",
		EngineMode:   "debate_full",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review","target_scope":"business","exclude_paths":["thirdparts"]}}`),
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}

	if !EnqueueTaskWithTriggerLog(nil, nil, repo.ID, repo.URL, taskType.ID, false, "test", models.RunParams{}) {
		t.Fatal("EnqueueTaskWithTriggerLog() = false, want true")
	}

	var report models.TaskReport
	if err := db.Where("repo_id = ?", repo.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.EngineMode != "debate_full" || report.ScanProfileHash == "" || report.EngineConfigHash == "" || report.PlannerVersion != "v1" {
		t.Fatalf("report execution snapshot incomplete: %+v", report)
	}
	if report.ExecutionSnapshotVersion != 1 || report.ExecutionSnapshotState != "complete" || len(report.ExecutionSnapshot) == 0 {
		t.Fatalf("report execution snapshot envelope incomplete: %+v", report)
	}
	if report.PromptContent == "" || report.PromptContentHash == "" || report.CategorySchemaHash == "" {
		t.Fatalf("report package snapshot incomplete: %+v", report)
	}
	var snapshot models.ExecutionContextSnapshot
	if err := json.Unmarshal(report.ExecutionSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal execution snapshot: %v", err)
	}
	if report.TaxonomySchemaVersion != snapshot.TaxonomySchemaVersion || report.TaxonomyHash != snapshot.TaxonomyHash {
		t.Fatalf("report taxonomy projection mismatch: report=%d/%q snapshot=%d/%q",
			report.TaxonomySchemaVersion, report.TaxonomyHash, snapshot.TaxonomySchemaVersion, snapshot.TaxonomyHash)
	}
	if snapshot.EngineMode != report.EngineMode || snapshot.ScanProfileHash != report.ScanProfileHash ||
		snapshot.PromptContentHash != report.PromptContentHash || snapshot.CategorySchemaHash != report.CategorySchemaHash {
		t.Fatalf("execution snapshot does not match report projections: %+v", snapshot)
	}
	var scanProfile profile.ScanProfile
	if err := json.Unmarshal(report.ScanProfile, &scanProfile); err != nil {
		t.Fatalf("unmarshal scan profile snapshot: %v", err)
	}
	if scanProfile.Name != profile.NameFullReview || scanProfile.TargetScope != "business" {
		t.Fatalf("unexpected scan profile snapshot: %+v", scanProfile)
	}
	if report.BaseCommit != "" || report.HeadCommit != "" {
		t.Fatalf("queue preset placeholder commits: base=%q head=%q", report.BaseCommit, report.HeadCommit)
	}
}
