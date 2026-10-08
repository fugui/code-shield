package defectlifecycle

import (
	"testing"
	"time"

	"code-shield/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpsertDefectAliasesReusesExistingStrongKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.DefectAlias{}); err != nil {
		t.Fatalf("migrate aliases: %v", err)
	}
	existing := models.DefectAlias{
		DefectID: 847, RepoID: 224, TaskTypeID: 13, AliasType: AliasK2,
		AliasValue: "strong-key", AliasClass: AliasStrong,
		FirstReportID: 19357, LastReportID: 19357, HitCount: 1,
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("seed alias: %v", err)
	}

	now := time.Now()
	input := LedgerInput{
		Report:   models.TaskReport{ID: 19822},
		Repo:     models.Repository{ID: 224},
		TaskType: models.TaskType{ID: 13},
	}
	identity := Identity{K2: "strong-key", NormPath: "src/a.cpp"}
	if err := upsertDefectAliases(db, input, 4618, identity, now); err != nil {
		t.Fatalf("upsert aliases: %v", err)
	}

	var count int64
	if err := db.Model(&models.DefectAlias{}).Where("alias_type = ? AND alias_value = ?", AliasK2, "strong-key").Count(&count).Error; err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one strong alias, got %d", count)
	}
	if err := db.First(&existing, existing.ID).Error; err != nil {
		t.Fatalf("reload alias: %v", err)
	}
	if existing.DefectID != 847 || existing.HitCount != 2 || existing.LastReportID != 19822 {
		t.Fatalf("unexpected alias update: %+v", existing)
	}
}

func TestUpsertDefectAliasesKeepsBucketAliasesPerDefect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.DefectAlias{}); err != nil {
		t.Fatalf("migrate aliases: %v", err)
	}
	now := time.Now()
	input := LedgerInput{
		Report:   models.TaskReport{ID: 19822},
		Repo:     models.Repository{ID: 224},
		TaskType: models.TaskType{ID: 13},
	}
	identity := Identity{NormPath: "src/a.cpp"}
	if err := upsertDefectAliases(db, input, 847, identity, now); err != nil {
		t.Fatalf("upsert first alias: %v", err)
	}
	if err := upsertDefectAliases(db, input, 4618, identity, now); err != nil {
		t.Fatalf("upsert second alias: %v", err)
	}

	var count int64
	if err := db.Model(&models.DefectAlias{}).Where("alias_type = ? AND alias_value = ?", AliasPath, "src/a.cpp").Count(&count).Error; err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected two bucket aliases, got %d", count)
	}
}
