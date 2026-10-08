package models_test

import (
	"errors"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestUpdateActiveTaskReportUpdatesNonTerminalReport(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_task_report_active_update", &models.TaskReport{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	report := models.TaskReport{Status: models.StatusAnalyzing, Score: 1}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	affected, err := models.UpdateActiveTaskReport(db, report.ID, map[string]interface{}{
		"status": models.StatusMerging,
		"score":  42,
	})
	if err != nil {
		t.Fatalf("UpdateActiveTaskReport() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if updated.Status != models.StatusMerging || updated.Score != 42 {
		t.Fatalf("status/score = %q/%d, want %q/42", updated.Status, updated.Score, models.StatusMerging)
	}
}

func TestUpdateActiveTaskReportRejectsTerminalReport(t *testing.T) {
	for _, status := range models.TerminalTaskStatuses() {
		t.Run(status, func(t *testing.T) {
			db := testdb.SetupIsolatedDB(t, "shield_task_report_immutable_"+status, &models.TaskReport{})
			if db == nil {
				t.Skip("Database not available, skipping DB test")
				return
			}

			report := models.TaskReport{Status: status, Score: 17}
			if err := db.Create(&report).Error; err != nil {
				t.Fatalf("create report: %v", err)
			}

			_, err := models.UpdateActiveTaskReport(db, report.ID, map[string]interface{}{
				"status":     models.StatusQueued,
				"ai_summary": "rewritten",
			})
			if !errors.Is(err, models.ErrTaskReportImmutable) {
				t.Fatalf("UpdateActiveTaskReport() error = %v, want %v", err, models.ErrTaskReportImmutable)
			}

			var unchanged models.TaskReport
			if err := db.First(&unchanged, report.ID).Error; err != nil {
				t.Fatalf("load report: %v", err)
			}
			if unchanged.Status != status || unchanged.AISummary != "" || unchanged.Score != 17 {
				t.Fatalf("terminal report changed: status=%q summary=%q score=%d", unchanged.Status, unchanged.AISummary, unchanged.Score)
			}
		})
	}
}
