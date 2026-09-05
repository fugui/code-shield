package defects

import (
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	origDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = t.TempDir()
	t.Cleanup(func() {
		models.AppConfig.Server.DataDir = origDir
	})
	return testdb.SetupIsolatedDB(t, "shield_defects_test")
}

func mustMigrate(t *testing.T, db *gorm.DB, dst ...interface{}) {
	t.Helper()
	if err := db.AutoMigrate(dst...); err != nil {
		t.Fatalf("mustMigrate failed: %v", err)
	}
}
