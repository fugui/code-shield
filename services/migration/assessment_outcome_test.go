package migration

import (
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestBackfillAssessmentOutcomesMapsLegacyStatuses(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_migration_assessment_outcomes", &models.AnalysisFinding{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	findings := []models.AnalysisFinding{
		{Title: "safe", AssessmentStatus: "valid"},
		{Title: "defect", AssessmentStatus: "invalid"},
		{Title: "not target", AssessmentStatus: "not_thread_creation"},
		{Title: "unknown", AssessmentStatus: "legacy_unknown"},
		{Title: "already migrated", AssessmentStatus: "invalid", AssessmentOutcome: "defect"},
	}
	if err := db.Create(&findings).Error; err != nil {
		t.Fatalf("create findings: %v", err)
	}
	if err := backfillAssessmentOutcomes(db); err != nil {
		t.Fatalf("backfillAssessmentOutcomes() error = %v", err)
	}

	var migrated []models.AnalysisFinding
	if err := db.Order("id").Find(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"safe":             "pass",
		"defect":           "defect",
		"not target":       "not_target",
		"unknown":          "",
		"already migrated": "defect",
	}
	for _, finding := range migrated {
		if finding.AssessmentOutcome != expected[finding.Title] {
			t.Fatalf("%s outcome = %q, want %q", finding.Title, finding.AssessmentOutcome, expected[finding.Title])
		}
	}

	if err := db.Model(&models.AnalysisFinding{}).Where("title = ?", "safe").
		Update("assessment_outcome", "override").Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillAssessmentOutcomes(db); err != nil {
		t.Fatalf("idempotent backfillAssessmentOutcomes() error = %v", err)
	}
	var safe models.AnalysisFinding
	if err := db.Where("title = ?", "safe").First(&safe).Error; err != nil {
		t.Fatal(err)
	}
	if safe.AssessmentOutcome != "override" {
		t.Fatalf("idempotent backfill overwrote outcome: %q", safe.AssessmentOutcome)
	}
}
