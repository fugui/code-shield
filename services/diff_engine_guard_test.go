package services

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"testing"
)

func TestDiffEngine_L1PhysicalFileGuard(t *testing.T) {
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

	repoID := uint(1001)
	taskTypeID := uint(2001)
	reportID1 := uint(3001)
	reportID2 := uint(3002)

	tmpRepo := t.TempDir()
	sourceFile := "src/sensor.cpp"
	fullSourcePath := filepath.Join(tmpRepo, sourceFile)
	if err := os.MkdirAll(filepath.Dir(fullSourcePath), 0755); err != nil {
		t.Fatal(err)
	}

	initialCode := `
void Sensor::Read() {
    int* p = nullptr;
    *p = 100;
}
`
	if err := os.WriteFile(fullSourcePath, []byte(initialCode), 0644); err != nil {
		t.Fatal(err)
	}

	// 第一次扫描：检出缺陷 A
	finding1 := []models.AnalysisFinding{
		{
			FilePath:    sourceFile,
			LineNumber:  "3-4",
			TriggerLine: "*p = 100;",
			ScopeSymbol: "Sensor::Read",
			Category:    "空指针解引用",
			Severity:    "CRITICAL",
			Title:       "空指针风险",
		},
	}

	_, err := DiffAndEnrichFindings(repoID, reportID1, taskTypeID, []string{sourceFile}, finding1, tmpRepo)
	if err != nil {
		t.Fatalf("First scan failed: %v", err)
	}

	var rec models.DefectFingerprintRecord
	if err := models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).First(&rec).Error; err != nil {
		t.Fatalf("Failed to query created record: %v", err)
	}
	if rec.Status != models.DiffStatusActive {
		t.Errorf("Expected status ACTIVE, got %s", rec.Status)
	}
	if rec.FileHashSnapshot == "" {
		t.Errorf("Expected non-empty FileHashSnapshot")
	}

	// 第二次扫描：代码文件物理上 100% 未动！但大模型由于漏报，findings 为空！
	var finding2 []models.AnalysisFinding
	_, err = DiffAndEnrichFindings(repoID, reportID2, taskTypeID, []string{sourceFile}, finding2, tmpRepo)
	if err != nil {
		t.Fatalf("Second scan failed: %v", err)
	}

	// 重新查询该缺陷状态：【L1 物理守卫应当生效】，绝对禁止标记为 RESOLVED！
	var recAfter models.DefectFingerprintRecord
	models.DB.Where("id = ?", rec.ID).First(&recAfter)

	if recAfter.Status != models.DiffStatusActive {
		t.Errorf("[CRITICAL] L1 Guard failed! Expected ACTIVE, but got %s (False Resolution!)", recAfter.Status)
	}
	if recAfter.MissedCount != 0 {
		t.Errorf("Expected MissedCount to stay 0 when file hash is identical, got %d", recAfter.MissedCount)
	}
}

func TestDiffEngine_L2ScopeGuard(t *testing.T) {
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

	repoID := uint(1002)
	taskTypeID := uint(2002)
	reportID1 := uint(3003)
	reportID2 := uint(3004)

	tmpRepo := t.TempDir()
	sourceFile := "src/processor.cpp"
	fullSourcePath := filepath.Join(tmpRepo, sourceFile)
	os.MkdirAll(filepath.Dir(fullSourcePath), 0755)

	initialCode := `
void Processor::Handle() {
    int* p = nullptr;
    *p = 100;
}
`
	os.WriteFile(fullSourcePath, []byte(initialCode), 0644)

	finding1 := []models.AnalysisFinding{
		{
			FilePath:    sourceFile,
			LineNumber:  "3-4",
			TriggerLine: "*p = 100;",
			ScopeSymbol: "Processor::Handle",
			Category:    "空指针解引用",
			Severity:    "HIGH",
			Title:       "空指针风险",
		},
	}

	DiffAndEnrichFindings(repoID, reportID1, taskTypeID, []string{sourceFile}, finding1, tmpRepo)

	var rec models.DefectFingerprintRecord
	models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).First(&rec)

	// 修改文件：但在文件尾部增加了一个不相干的函数，Processor::Handle 本身一行没动！
	modifiedCode := `
void Processor::Handle() {
    int* p = nullptr;
    *p = 100;
}

void OtherClass::UnrelatedMethod() {
    // 新增的不相干代码
}
`
	os.WriteFile(fullSourcePath, []byte(modifiedCode), 0644)

	// 第二次扫描：本次未报出 Processor::Handle
	var finding2 []models.AnalysisFinding
	DiffAndEnrichFindings(repoID, reportID2, taskTypeID, []string{sourceFile}, finding2, tmpRepo)

	var recAfter models.DefectFingerprintRecord
	models.DB.Where("id = ?", rec.ID).First(&recAfter)

	// 【L2 守卫应当生效】：因为 Processor::Handle 函数体没变，仍然强制保持 ACTIVE！
	if recAfter.Status != models.DiffStatusActive {
		t.Errorf("[CRITICAL] L2 Guard failed! Expected ACTIVE, but got %s", recAfter.Status)
	}
}

func TestDiffEngine_GracePeriodAndVerifiedPending(t *testing.T) {
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

	repoID := uint(1003)
	taskTypeID := uint(2003)
	reportID1 := uint(3005)
	reportID2 := uint(3006)
	reportID3 := uint(3007)

	tmpRepo := t.TempDir()
	sourceFile := "src/worker.cpp"
	fullSourcePath := filepath.Join(tmpRepo, sourceFile)
	os.MkdirAll(filepath.Dir(fullSourcePath), 0755)

	initialCode := `
void Worker::Run() {
    int* p = nullptr;
    *p = 42; // bug here
}
`
	os.WriteFile(fullSourcePath, []byte(initialCode), 0644)

	finding1 := []models.AnalysisFinding{
		{
			FilePath:    sourceFile,
			LineNumber:  "3-4",
			TriggerLine: "*p = 42;",
			ScopeSymbol: "Worker::Run",
			Category:    "空指针解引用",
			Severity:    "CRITICAL", // 严重级别
			Title:       "空指针崩溃",
		},
	}

	DiffAndEnrichFindings(repoID, reportID1, taskTypeID, []string{sourceFile}, finding1, tmpRepo)

	var rec models.DefectFingerprintRecord
	models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).First(&rec)

	// 真实修复：修改代码为判空安全代码
	fixedCode := `
void Worker::Run() {
    int val = 42;
    int* p = &val;
    *p = 42;
}
`
	os.WriteFile(fullSourcePath, []byte(fixedCode), 0644)

	// 扫描 2（第 1 次未检出）：进入观察期，missed_count = 1，状态依然 ACTIVE！
	var finding2 []models.AnalysisFinding
	DiffAndEnrichFindings(repoID, reportID2, taskTypeID, []string{sourceFile}, finding2, tmpRepo)

	var recScan2 models.DefectFingerprintRecord
	models.DB.Where("id = ?", rec.ID).First(&recScan2)
	if recScan2.Status != models.DiffStatusActive {
		t.Errorf("Expected scan 2 to stay ACTIVE in grace period, got %s", recScan2.Status)
	}
	if recScan2.MissedCount != 1 {
		t.Errorf("Expected MissedCount=1, got %d", recScan2.MissedCount)
	}

	// 扫描 3（第 2 次未检出）：达到阈值 2！因为是 CRITICAL 级，流转为 VERIFIED_PENDING！
	var finding3 []models.AnalysisFinding
	DiffAndEnrichFindings(repoID, reportID3, taskTypeID, []string{sourceFile}, finding3, tmpRepo)

	var recScan3 models.DefectFingerprintRecord
	models.DB.Where("id = ?", rec.ID).First(&recScan3)
	if recScan3.Status != models.DiffStatusVerifiedPending {
		t.Errorf("Expected scan 3 to be VERIFIED_PENDING for CRITICAL defect, got %s", recScan3.Status)
	}
	if recScan3.ResolvedAt == nil {
		t.Errorf("Expected ResolvedAt to be set")
	}
}
