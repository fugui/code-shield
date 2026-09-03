package services

import (
	"code-shield/models"
	"testing"
)

func TestDiffClassification(t *testing.T) {
	// 验证常量定义
	if models.DiffStatusNew != "NEW" {
		t.Errorf("Expected NEW, got %s", models.DiffStatusNew)
	}
	if models.DiffStatusExisted != "EXISTED" {
		t.Errorf("Expected EXISTED, got %s", models.DiffStatusExisted)
	}
	if models.DiffStatusResolved != "RESOLVED" {
		t.Errorf("Expected RESOLVED, got %s", models.DiffStatusResolved)
	}
	if models.DiffStatusReopened != "REOPENED" {
		t.Errorf("Expected REOPENED, got %s", models.DiffStatusReopened)
	}
}

func TestDiffAndEnrichFindings_DuplicateFingerprint(t *testing.T) {
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

	repoID := uint(9999)
	taskTypeID := uint(8888)
	taskReportID := uint(7777)

	defer func() {
		models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Delete(&models.DefectFingerprintRecord{})
	}()

	// 模拟单次扫描中返回两条相同特征的缺陷（例如空路径或同一处触发语句）
	findings := []models.AnalysisFinding{
		{
			FilePath:    "src/duplicate.cpp",
			LineNumber:  "10-20",
			TriggerLine: "p->init();",
			ScopeSymbol: "Init()",
			Category:    "CWE-476",
			Title:       "空指针风险1",
		},
		{
			FilePath:    "src/duplicate.cpp",
			LineNumber:  "10-20",
			TriggerLine: "p->init();",
			ScopeSymbol: "Init()",
			Category:    "CWE-476",
			Title:       "空指针风险2（相同指纹）",
		},
	}

	enriched, err := DiffAndEnrichFindings(repoID, taskReportID, taskTypeID, []string{"src/duplicate.cpp"}, findings)
	if err != nil {
		t.Fatalf("DiffAndEnrichFindings failed with duplicate fingerprint: %v", err)
	}

	if len(enriched) != 2 {
		t.Fatalf("expected 2 enriched findings, got %d", len(enriched))
	}

	// 第一条应为 NEW，第二条循环内应同步命中内存 recordMap 标记为 EXISTED，不触发唯一索引冲突
	if enriched[0].DiffStatus != models.DiffStatusNew {
		t.Errorf("expected first finding to be NEW, got %s", enriched[0].DiffStatus)
	}
	if enriched[1].DiffStatus != models.DiffStatusExisted {
		t.Errorf("expected second finding to be EXISTED, got %s", enriched[1].DiffStatus)
	}

	// 数据库中应仅有 1 条指纹记录，绝不发生 duplicate key 冲突
	var count int64
	models.DB.Model(&models.DefectFingerprintRecord{}).Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record in defect_fingerprint_records, got %d", count)
	}
}
