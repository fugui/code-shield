package queue

import (
	"encoding/json"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestEnqueueTaskCreatesAndBindsTaskTypeRevision(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_task_type_revision",
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

	repo := models.Repository{Name: "revision-repo-" + time.Now().Format("150405.000000000"), URL: "http://example.com/revision.git"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	taskType := models.TaskType{
		Name:         "deep_review",
		EngineMode:   "debate_full",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review","target_scope":"business"}}`),
		AssessmentConfig: []byte(`{
			"version":1,
			"profile":"full_review",
			"schema":"code-shield.assessment-config.v1",
			"domain":"revision"
		}`),
		Categories:       []byte(`["Correctness"]`),
		DomainLabel:      "修订治理",
		TargetSemantics:  []byte(`{"singular":"revision","plural":"revisions"}`),
		DisplaySemantics: []byte(`{"target_label":"revision"}`),
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}

	if !EnqueueTaskWithTriggerLog(nil, nil, repo.ID, repo.URL, taskType.ID, false, "test", models.RunParams{}) {
		t.Fatal("EnqueueTaskWithTriggerLog() = false, want true")
	}

	var revisions []models.TaskTypeRevision
	if err := db.Where("task_type_id = ?", taskType.ID).Order("revision asc").Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].Revision != 1 || revisions[0].AggregateHash == "" ||
		revisions[0].PromptContentHash == "" || revisions[0].CategorySchemaHash == "" ||
		revisions[0].PostprocessHash != "" {
		t.Fatalf("unexpected first revision: %+v", revisions)
	}
	firstRevision := revisions[0]
	if taskType.CurrentRevisionID == nil || *taskType.CurrentRevisionID != firstRevision.ID {
		db.First(&taskType, taskType.ID)
		if taskType.CurrentRevisionID == nil || *taskType.CurrentRevisionID != firstRevision.ID {
			t.Fatalf("current revision not bound: %+v", taskType.CurrentRevisionID)
		}
	}

	var report models.TaskReport
	if err := db.Where("repo_id = ?", repo.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.TaskTypeRevisionID == nil || *report.TaskTypeRevisionID != firstRevision.ID {
		t.Fatalf("report revision = %+v, want %d", report.TaskTypeRevisionID, firstRevision.ID)
	}
	var snapshot models.ExecutionContextSnapshot
	if err := json.Unmarshal(report.ExecutionSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal execution snapshot: %v", err)
	}
	if snapshot.TaskTypeRevisionID != "1" || snapshot.EngineConfigHash != firstRevision.EngineConfigHash ||
		snapshot.AssessmentConfigHash != firstRevision.AssessmentConfigHash ||
		snapshot.PromptContentHash != firstRevision.PromptContentHash ||
		snapshot.CategorySchemaHash != firstRevision.CategorySchemaHash ||
		snapshot.PostprocessHash != firstRevision.PostprocessHash {
		t.Fatalf("snapshot revision hashes mismatch: %+v %+v", snapshot, firstRevision)
	}
	var targetSemantics map[string]string
	if err := json.Unmarshal(firstRevision.TargetSemantics, &targetSemantics); err != nil {
		t.Fatalf("unmarshal revision target semantics: %v", err)
	}
	if firstRevision.DomainLabel != "修订治理" || targetSemantics["singular"] != "revision" || targetSemantics["plural"] != "revisions" {
		t.Fatalf("revision semantic metadata mismatch: %+v %+v", firstRevision.DomainLabel, targetSemantics)
	}
	var snapshotTarget map[string]string
	if err := json.Unmarshal(snapshot.TargetSemantics, &snapshotTarget); err != nil {
		t.Fatalf("unmarshal snapshot target semantics: %v", err)
	}
	if snapshot.DomainLabel != "修订治理" || snapshotTarget["singular"] != "revision" {
		t.Fatalf("snapshot semantic metadata mismatch: %+v %+v", snapshot.DomainLabel, snapshotTarget)
	}
	if report.DomainLabel != "修订治理" || len(report.TargetSemantics) == 0 || len(report.DisplaySemantics) == 0 {
		t.Fatalf("report semantic metadata incomplete: %+v", report)
	}
	var reportTarget map[string]string
	if err := json.Unmarshal(report.TargetSemantics, &reportTarget); err != nil {
		t.Fatalf("unmarshal report target semantics: %v", err)
	}
	if reportTarget["singular"] != "revision" {
		t.Fatalf("report target semantics = %+v, want revision", reportTarget)
	}

	updatedAssessmentConfig := []byte(`{
		"version":1,
		"profile":"full_review",
		"schema":"code-shield.assessment-config.v1",
		"domain":"revision-v2"
	}`)
	if err := db.Model(&taskType).Updates(map[string]any{
		"assessment_config":      string(updatedAssessmentConfig),
		"assessment_config_hash": models.HashAssessmentConfig(updatedAssessmentConfig),
		"current_revision_id":    nil,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TaskReport{}).Where("id = ?", report.ID).
		Update("status", models.StatusSuccess).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TaskExecutionLog{}).Where("task_report_id = ?", report.ID).
		Update("status", models.StatusSuccess).Error; err != nil {
		t.Fatal(err)
	}
	if !EnqueueTaskWithTriggerLog(nil, nil, repo.ID, repo.URL, taskType.ID, false, "test", models.RunParams{}) {
		t.Fatal("second EnqueueTaskWithTriggerLog() = false, want true")
	}
	revisions = nil
	if err := db.Where("task_type_id = ?", taskType.ID).Order("revision asc").Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].ID != firstRevision.ID || revisions[1].ID == firstRevision.ID {
		t.Fatalf("assessment config change did not create an immutable revision: %+v", revisions)
	}
}
