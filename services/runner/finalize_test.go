package runner

import (
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestFinalizeFailsWhenAllPrimaryChunksFailed(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_finalize_all_failed",
		&models.TaskReport{},
		&models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	tempDir := t.TempDir()
	report := models.TaskReport{Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	ctx := &TaskContext{
		Report:     report,
		ReportPath: tempDir + "/report.md",
		JsonPath:   tempDir + "/summary.json",
		Summary:    TaskSummaryReport{},
		TaskType:   models.TaskType{},
		AutoNotify: false,
	}
	ctx.Summary.Analysis.TotalChunks = 3
	ctx.Summary.Analysis.FailedChunks = 3
	ctx.Summary.Analysis.Status = "failed"

	if err := Finalize(ctx, TaskResult{}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if updated.Status != models.StatusFailed {
		t.Fatalf("status = %q, want %q", updated.Status, models.StatusFailed)
	}
}

func TestFinalizeUpdatesReportCreatedAtOnCompletion(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_finalize_created_at",
		&models.TaskReport{}, &models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	tempDir := t.TempDir()
	createdAt := time.Now().Add(-2 * time.Second)
	report := models.TaskReport{Status: models.StatusAnalyzing, CreatedAt: createdAt}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	ctx := &TaskContext{
		Report:     report,
		ReportPath: tempDir + "/report.md",
		JsonPath:   tempDir + "/summary.json",
		Summary:    TaskSummaryReport{},
		TaskType:   models.TaskType{},
	}

	if err := Finalize(ctx, TaskResult{Score: 90}); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if !updated.CreatedAt.After(createdAt) {
		t.Fatalf("created_at = %v, want time after %v", updated.CreatedAt, createdAt)
	}

	if err := MarkFailed(ctx, "test failure"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	var failed models.TaskReport
	if err := db.First(&failed, report.ID).Error; err != nil {
		t.Fatalf("load failed report: %v", err)
	}
	if !failed.CreatedAt.Equal(createdAt) {
		t.Fatalf("failed created_at = %v, want %v", failed.CreatedAt, createdAt)
	}
}
