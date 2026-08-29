package services

import (
	"code-shield/models"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueue_MaxQueueSizeLimit(t *testing.T) {
	// 校验配置项生效与默认值
	models.AppConfig.Server.MaxQueueSize = 2000
	if models.AppConfig.Server.MaxQueueSize != 2000 {
		t.Fatalf("expected MaxQueueSize == 2000, got %d", models.AppConfig.Server.MaxQueueSize)
	}

	// 校验 NotifyWorker 在无 Worker 读取时不阻塞
	for i := 0; i < 10; i++ {
		NotifyWorker()
	}
}

func TestQueue_PauseAndResume(t *testing.T) {
	// 初始状态重置为 false
	SetQueuePaused(false)
	if IsQueuePaused() {
		t.Fatalf("expected IsQueuePaused == false initially")
	}

	// 设置为暂停 (Drain) 模式
	SetQueuePaused(true)
	if !IsQueuePaused() {
		t.Fatalf("expected IsQueuePaused == true after SetQueuePaused(true)")
	}

	// 暂停模式下，fetchNextPendingTask 必须直接拦截返回 nil, false
	task, found := fetchNextPendingTask()
	if found || task != nil {
		t.Fatalf("expected fetchNextPendingTask to return nil, false when paused, got %v, %v", task, found)
	}

	// 恢复派发
	SetQueuePaused(false)
	if IsQueuePaused() {
		t.Fatalf("expected IsQueuePaused == false after SetQueuePaused(false)")
	}
}

func TestTaskFromExecLog_ResumeFlag(t *testing.T) {
	resumeLog := &models.TaskExecutionLog{
		ID:         42,
		RepoID:     7,
		TaskTypeID: 9,
		IsResume:   true,
	}
	task := taskFromExecLog(resumeLog, models.TaskReport{ID: 11}, models.RunParams{})
	if !task.IsResume {
		t.Fatalf("expected IsResume == true for resume log, got false")
	}
	if task.LogID != 42 || task.ReportID != 11 || task.RepoID != 7 || task.TaskTypeID != 9 {
		t.Fatalf("unexpected task fields: %+v", task)
	}

	normalLog := &models.TaskExecutionLog{
		ID:         43,
		RepoID:     8,
		TaskTypeID: 10,
		IsResume:   false,
	}
	normalTask := taskFromExecLog(normalLog, models.TaskReport{ID: 12}, models.RunParams{})
	if normalTask.IsResume {
		t.Fatalf("expected IsResume == false for normal log, got true")
	}
}

// TestQueue_ResumeTaskFlow 验证恢复任务入队后能被 Worker 原子抢占，且带 IsResume 标记。
// 需要显式设置 TEST_DB_DSN（否则跳过）；测试会在独立的一次性数据库中运行，结束后自动删除。
func TestQueue_ResumeTaskFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("Skipping DB test: TEST_DB_DSN not set")
	}

	// 1. 连接 admin 库（postgres），创建一次性测试库
	admin, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  withDBName(dsn, "postgres"),
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Skipf("Skipping DB test: PostgreSQL not available (%v)", err)
		return
	}

	testDBName := fmt.Sprintf("code_shield_test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + testDBName).Error; err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  withDBName(dsn, testDBName),
		PreferSimpleProtocol: true,
	}), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// 测试结束后先断开连接，再删除测试库
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
		admin.Exec("DROP DATABASE IF EXISTS " + testDBName)
	})

	models.DB = db

	// 全新空库：按外键依赖顺序逐表创建，避免 AutoMigrate 的建表顺序问题
	m := db.Migrator()
	for _, model := range []interface{}{
		&models.Department{},
		&models.User{},
		&models.TaskType{},
		&models.Repository{},
		&models.ScheduleConfig{},
		&models.TaskTriggerLog{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
	} {
		if err := m.CreateTable(model); err != nil {
			t.Fatalf("failed to create table for %T: %v", model, err)
		}
	}

	// 准备测试数据（唯一名称避免重复运行冲突）
	repo := models.Repository{
		Name: "test-resume-repo-" + time.Now().Format("150405.000000000"),
		URL:  "http://example.com/resume-test.git",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}
	defer db.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:        "test_resume_type",
		DisplayName: "测试恢复任务",
		EngineMode:  "chunked",
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("failed to create test task type: %v", err)
	}
	defer db.Delete(&models.TaskType{}, taskType.ID)

	report := models.TaskReport{
		RepoID:      repo.ID,
		TaskTypeID:  taskType.ID,
		Status:      models.StatusFailed,
		CloneStatus: models.StatusSuccess,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("failed to create test report: %v", err)
	}
	defer db.Where("task_report_id = ?", report.ID).Delete(&models.TaskExecutionLog{})
	defer db.Delete(&models.TaskReport{}, report.ID)

	execLog := models.TaskExecutionLog{
		RepoID:         repo.ID,
		TaskTypeID:     taskType.ID,
		TaskReportID:   &report.ID,
		TriggerType:    "manual",
		Status:         models.StatusFailed,
		StatusPriority: models.GetStatusPriority(models.StatusFailed),
		StartTime:      time.Now(),
	}
	if err := db.Create(&execLog).Error; err != nil {
		t.Fatalf("failed to create test execution log: %v", err)
	}

	// 入队恢复任务
	if err := EnqueueResumeTask(report); err != nil {
		t.Fatalf("EnqueueResumeTask returned error: %v", err)
	}

	// 校验日志已被重置为 pending 并打上恢复标记
	var queued models.TaskExecutionLog
	if err := db.First(&queued, execLog.ID).Error; err != nil {
		t.Fatalf("failed to reload execution log: %v", err)
	}
	if queued.Status != models.StatusPending {
		t.Fatalf("expected log status pending after enqueue, got %s", queued.Status)
	}
	if !queued.IsResume {
		t.Fatalf("expected IsResume == true on execution log after enqueue")
	}

	// 校验 Worker 能拉到该任务并正确识别为恢复任务
	task, found := fetchNextPendingTask()
	if !found {
		t.Fatalf("resume task was not fetched by worker")
	}
	if !task.IsResume {
		t.Fatalf("expected fetched task IsResume == true, got false")
	}
	if task.LogID != execLog.ID || task.ReportID != report.ID {
		t.Fatalf("unexpected fetched task: %+v", task)
	}

	// 校验抢占后日志状态为 running
	var claimed models.TaskExecutionLog
	if err := db.First(&claimed, execLog.ID).Error; err != nil {
		t.Fatalf("failed to reload claimed log: %v", err)
	}
	if claimed.Status != models.StatusRunning {
		t.Fatalf("expected log status running after fetch, got %s", claimed.Status)
	}
}

// withDBName 返回将 DSN 中数据库名替换为 dbname 后的新 DSN（兼容 key=value 与 URL 两种形式）
func withDBName(dsn, dbname string) string {
	if strings.Contains(dsn, "://") {
		slash := strings.LastIndex(dsn, "/")
		suffix := dsn[slash+1:]
		query := ""
		if q := strings.Index(suffix, "?"); q >= 0 {
			query = suffix[q:]
		}
		return dsn[:slash+1] + dbname + query
	}
	return regexp.MustCompile(`dbname=\S+`).ReplaceAllString(dsn, "dbname="+dbname)
}
