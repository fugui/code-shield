package runner

import (
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestUpdateTaskStatusAndProgress(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_telemetry_test",
		&models.TaskReport{},
		&models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	// 1. 创建处于 cloning 状态的 TaskReport 和 TaskExecutionLog
	report := models.TaskReport{
		Status:      models.StatusCloning,
		TotalChunks: 5,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("failed to create test report: %v", err)
	}

	execLog := models.TaskExecutionLog{
		TaskReportID:   &report.ID,
		Status:         models.StatusCloning,
		StatusPriority: models.GetStatusPriority(models.StatusCloning),
	}
	if err := db.Create(&execLog).Error; err != nil {
		t.Fatalf("failed to create test execution log: %v", err)
	}

	// 2. 测试 UpdateTaskStatus 显式跃迁至 pre_processing
	UpdateTaskStatus(report.ID, models.StatusPreProcessing)

	var checkReport models.TaskReport
	var checkLog models.TaskExecutionLog
	db.First(&checkReport, report.ID)
	db.First(&checkLog, execLog.ID)

	if checkReport.Status != models.StatusPreProcessing {
		t.Errorf("expected report status %s, got %s", models.StatusPreProcessing, checkReport.Status)
	}
	if checkLog.Status != models.StatusPreProcessing {
		t.Errorf("expected log status %s, got %s", models.StatusPreProcessing, checkLog.Status)
	}

	// 3. 将状态重置为 cloning，模拟引擎在此前漏流转时，触发 UpdateTaskProgress 的自动自愈跃迁
	db.Model(&checkReport).Update("status", models.StatusCloning)
	db.Model(&checkLog).Update("status", models.StatusCloning)

	UpdateTaskProgress(report.ID, 5, 2, 2, "chunk_2")

	db.First(&checkReport, report.ID)
	db.First(&checkLog, execLog.ID)

	// 校验分片微进度与自愈跃迁为 analyzing
	if checkReport.Status != models.StatusAnalyzing {
		t.Errorf("expected auto-healed report status %s, got %s", models.StatusAnalyzing, checkReport.Status)
	}
	if checkReport.TotalChunks != 5 || checkReport.ProcessedChunks != 2 || checkReport.SuccessChunks != 2 {
		t.Errorf("expected chunks (5, 2, 2), got (%d, %d, %d)",
			checkReport.TotalChunks, checkReport.ProcessedChunks, checkReport.SuccessChunks)
	}
	if checkLog.Status != models.StatusAnalyzing {
		t.Errorf("expected log status %s, got %s", models.StatusAnalyzing, checkLog.Status)
	}

	// 4. 测试 UpdateTaskStatus 跃迁至 synthesis
	UpdateTaskStatus(report.ID, models.StatusSynthesis)
	db.First(&checkReport, report.ID)
	db.First(&checkLog, execLog.ID)

	if checkReport.Status != models.StatusSynthesis || checkLog.Status != models.StatusSynthesis {
		t.Errorf("expected status %s, got report=%s, log=%s",
			models.StatusSynthesis, checkReport.Status, checkLog.Status)
	}
}

func TestUpdateTaskStatusNilSafety(t *testing.T) {
	oldDB := models.DB
	models.DB = nil
	defer func() { models.DB = oldDB }()

	// 验证在 models.DB 为空时不发生 panic
	UpdateTaskStatus(0, models.StatusAnalyzing)
	UpdateTaskStatus(123, models.StatusAnalyzing)
	UpdateTaskProgress(0, 5, 1, 1, "test")
	UpdateTaskProgress(123, 5, 1, 1, "test")
}
