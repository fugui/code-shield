package handlers

import (
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestDailyStatsAggregatesDisplayedMetrics(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_debug_daily_stats",
		&models.TaskReport{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	today := time.Now()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	yesterday := todayStart.Add(-24 * time.Hour)

	statuses := []string{
		models.StatusSuccess,
		models.StatusFailed,
		models.StatusDegraded,
		models.StatusSkipped,
		models.StatusQueued,
	}
	for _, status := range statuses {
		report := models.TaskReport{Status: status, Tier1Tokens: 10, Tier2Tokens: 20}
		if status == models.StatusQueued {
			report.CreatedAt = yesterday
		}
		if err := db.Create(&report).Error; err != nil {
			t.Fatalf("create %s report: %v", status, err)
		}
	}
	stats := loadDailyStats(db, todayStart)
	if stats.Total != 4 || stats.Success != 2 || stats.Failed != 1 {
		t.Fatalf("daily stats = %+v", stats)
	}
	if stats.Tier1Tokens != 40 || stats.Tier2Tokens != 80 {
		t.Fatalf("token stats = %+v", stats)
	}
}
