package services

import (
	"code-shield/models"
	"testing"
)

func TestRunFingerprintMigration_ConvergenceAndFeedbackInheritance(t *testing.T) {
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()

	mustMigrate(t, models.DB,
		&models.Department{},
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.DefectFingerprintRecord{},
	)

	repoID := uint(70001)
	taskTypeID := uint(80001)

	defer func() {
		models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Delete(&models.DefectFingerprintRecord{})
	}()

	// 模拟存量数据：两条记录物理位置完全相同，但旧算法因 category 漂移生成了两个不同的旧指纹
	oldRecord1 := models.DefectFingerprintRecord{
		RepoID:         repoID,
		TaskTypeID:     taskTypeID,
		Fingerprint:    "old_fp_english_cwe476",
		FilePath:       "src/network/client.cpp",
		ScopeSymbol:    "Network::Client::Connect",
		TriggerLine:    "pSocket->init();",
		Category:       "CWE-476: NULL Pointer Dereference",
		Status:         models.DiffStatusActive,
		FeedbackStatus: "UNREVIEWED",
	}

	oldRecord2 := models.DefectFingerprintRecord{
		RepoID:         repoID,
		TaskTypeID:     taskTypeID,
		Fingerprint:    "old_fp_chinese_null_deref",
		FilePath:       "src/network/client.cpp",
		ScopeSymbol:    "Client::Connect", // 命名空间差异
		TriggerLine:    "pSocket->init();",
		Category:       "空指针解引用", // 分类差异
		Status:         models.DiffStatusActive,
		FeedbackStatus: "FALSE_POSITIVE", // 人工辛苦标记的误报！
		FeedbackReason: "已经在外层做过防御性断言保护",
	}

	if err := models.DB.Create(&oldRecord1).Error; err != nil {
		t.Fatalf("Failed to seed old record 1: %v", err)
	}
	if err := models.DB.Create(&oldRecord2).Error; err != nil {
		t.Fatalf("Failed to seed old record 2: %v", err)
	}

	// 1. 测试 Dry-Run 模式
	dryRes, err := RunFingerprintMigration(models.DB, "", true)
	if err != nil {
		t.Fatalf("Dry run failed: %v", err)
	}
	if dryRes.TotalRecords != 2 {
		t.Errorf("Expected 2 total records, got %d", dryRes.TotalRecords)
	}
	if dryRes.MergedCount != 1 {
		t.Errorf("Expected 1 merged conflict, got %d", dryRes.MergedCount)
	}

	// 验证 dry-run 后数据库未变动
	var checkRec1 models.DefectFingerprintRecord
	models.DB.Where("id = ?", oldRecord1.ID).First(&checkRec1)
	if checkRec1.Fingerprint != "old_fp_english_cwe476" {
		t.Errorf("Dry run should not modify records in DB")
	}

	// 2. 测试 Live 迁移模式
	liveRes, err := RunFingerprintMigration(models.DB, "", false)
	if err != nil {
		t.Fatalf("Live migration failed: %v", err)
	}
	if liveRes.MigratedCount != 1 || liveRes.MergedCount != 1 {
		t.Errorf("Expected MigratedCount=1, MergedCount=1, got Migrated=%d, Merged=%d",
			liveRes.MigratedCount, liveRes.MergedCount)
	}

	// 3. 验证迁移与合并结果
	var activeRecords []models.DefectFingerprintRecord
	models.DB.Where("repo_id = ? AND task_type_id = ? AND status = ?", repoID, taskTypeID, models.DiffStatusActive).Find(&activeRecords)
	if len(activeRecords) != 1 {
		t.Fatalf("Expected exactly 1 ACTIVE record after convergence, got %d", len(activeRecords))
	}

	survivor := activeRecords[0]
	// 验证指纹已更新为新纯物理公式哈希
	expectedFP := CalculateDefectFingerprint(repoID, taskTypeID, "src/network/client.cpp", "psocket->init()", "Client::Connect")
	if survivor.Fingerprint != expectedFP {
		t.Errorf("Expected updated fingerprint %s, got %s", expectedFP, survivor.Fingerprint)
	}

	// 验证人工反馈 100% 继承！
	if survivor.FeedbackStatus != "FALSE_POSITIVE" {
		t.Errorf("Expected FeedbackStatus FALSE_POSITIVE to be inherited, got %s", survivor.FeedbackStatus)
	}
	if survivor.FeedbackReason != "已经在外层做过防御性断言保护" {
		t.Errorf("Expected FeedbackReason to be inherited, got %s", survivor.FeedbackReason)
	}

	// 验证冗余记录已标记为已归档
	var archivedRecords []models.DefectFingerprintRecord
	models.DB.Where("repo_id = ? AND task_type_id = ? AND status = 'MERGED_ARCHIVED'", repoID, taskTypeID).Find(&archivedRecords)
	if len(archivedRecords) != 1 {
		t.Errorf("Expected 1 MERGED_ARCHIVED record, got %d", len(archivedRecords))
	}
}
