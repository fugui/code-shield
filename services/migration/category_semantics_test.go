package migration

import (
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestBackfillCategorySemantics(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_category_semantics_backfill", &models.AnalysisFinding{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	findings := []models.AnalysisFinding{
		{Title: "legacy", Severity: "严重", Category: "legacy label", CategoryCode: "", CategoryStatus: ""},
		{Title: "governed", Severity: "严重", Category: "governed", CategoryCode: "MEM_USE_AFTER_FREE", CategoryStatus: ""},
		{Title: "preserved", Severity: "一般", Category: "review", CategoryCode: "", CategoryStatus: "REVIEW_REQUIRED"},
	}
	for i := range findings {
		if err := db.Create(&findings[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := BackfillCategorySemantics(db); err != nil {
		t.Fatal(err)
	}
	var legacyCount, validCount, preservedCount int64
	if err := db.Model(&models.AnalysisFinding{}).Where("title = ? AND category_status = ?", "legacy", "LEGACY").Count(&legacyCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AnalysisFinding{}).Where("title = ? AND category_status = ?", "governed", "VALID").Count(&validCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AnalysisFinding{}).Where("title = ? AND category_status = ?", "preserved", "REVIEW_REQUIRED").Count(&preservedCount).Error; err != nil {
		t.Fatal(err)
	}
	if legacyCount != 1 || validCount != 1 || preservedCount != 1 {
		t.Fatalf("backfill counts legacy=%d valid=%d preserved=%d", legacyCount, validCount, preservedCount)
	}
}
