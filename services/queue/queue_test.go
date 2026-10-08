package queue

import (
	"code-common/backend/testdb"
	"code-shield/models"
	"errors"
	"testing"
	"time"
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

func TestQueue_ResizeWorkerPoolHotReload(t *testing.T) {
	workerCountLock.Lock()
	workerCount = 3
	workerStops = map[int]chan struct{}{
		1: make(chan struct{}),
		2: make(chan struct{}),
		3: make(chan struct{}),
	}
	workerCountLock.Unlock()
	defer func() {
		workerCountLock.Lock()
		workerCount = 0
		for _, stop := range workerStops {
			close(stop)
		}
		workerStops = make(map[int]chan struct{})
		dones := make([]chan struct{}, 0, len(workerDones))
		for _, done := range workerDones {
			dones = append(dones, done)
		}
		workerCountLock.Unlock()
		for _, done := range dones {
			<-done
		}
	}()

	if got := WorkerPoolSize(); got != 3 {
		t.Fatalf("expected initial worker pool size 3, got %d", got)
	}

	workerCountLock.RLock()
	if len(workerStops) != 3 {
		workerCountLock.RUnlock()
		t.Fatalf("expected 3 active worker handles, got %d", len(workerStops))
	}
	workerCountLock.RUnlock()

	ResizeWorkerPool(5)
	if got := WorkerPoolSize(); got != 5 {
		t.Fatalf("expected worker pool size 5 after scale up, got %d", got)
	}

	ResizeWorkerPool(2)
	if got := WorkerPoolSize(); got != 2 {
		t.Fatalf("expected worker pool size 2 after scale down, got %d", got)
	}

	workerCountLock.RLock()
	activeHandles := len(workerStops)
	workerCountLock.RUnlock()
	if activeHandles != 2 {
		t.Fatalf("expected 2 active worker handles after scale down, got %d", activeHandles)
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

func TestEnqueueResumeTaskRejectsTerminalReport(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_resume_terminal", &models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskExecutionLog{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}

	repo := models.Repository{Name: "terminal-resume-repo", URL: "https://example.com/terminal.git", IsActive: true}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	taskType := models.TaskType{Name: "terminal_resume_type", DisplayName: "终态恢复测试", EngineMode: "chunked"}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSuccess,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}

	if err := EnqueueResumeTask(report); !errors.Is(err, ErrResumeTerminalReport) {
		t.Fatalf("EnqueueResumeTask() error = %v, want %v", err, ErrResumeTerminalReport)
	}

	var logCount int64
	db.Table("task_execution_logs").Where("task_report_id = ?", report.ID).Count(&logCount)
	if logCount != 0 {
		t.Fatalf("execution logs = %d, want 0", logCount)
	}

	var unchanged models.TaskReport
	if err := db.First(&unchanged, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if unchanged.Status != models.StatusSuccess {
		t.Fatalf("status = %q, want %q", unchanged.Status, models.StatusSuccess)
	}
}

func TestEnqueueTaskRejectsActiveReportConcurrently(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_enqueue_concurrency",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "enqueue-concurrency-repo", URL: "https://example.com/enqueue-concurrency.git"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	taskType := models.TaskType{
		Name:         "enqueue_concurrency_type",
		DisplayName:  "入队并发测试",
		EngineMode:   "chunked",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review","target_scope":"business"}}`),
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	if err := models.EnsureActiveTaskReportUniqueIndex(db); err != nil {
		t.Fatalf("ensure active report index: %v", err)
	}

	const attempts = 2
	results := make(chan bool, attempts)
	for range attempts {
		go func() {
			results <- EnqueueTaskWithTriggerLog(nil, nil, repo.ID, repo.URL, taskType.ID, false, "test", models.RunParams{})
		}()
	}

	enqueued := 0
	for range attempts {
		if <-results {
			enqueued++
		}
	}
	if enqueued != 1 {
		t.Fatalf("concurrent enqueue success count = %d, want 1", enqueued)
	}

	var logCount, reportCount int64
	db.Model(&models.TaskExecutionLog{}).Where("repo_id = ?", repo.ID).Count(&logCount)
	db.Model(&models.TaskReport{}).Where("repo_id = ?", repo.ID).Count(&reportCount)
	if logCount != 1 || reportCount != 1 {
		t.Fatalf("log/report count = %d/%d, want 1/1", logCount, reportCount)
	}
}

func TestEnqueueResumeTaskRejectsBusyReport(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_resume_busy",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "busy-resume-repo", URL: "https://example.com/busy.git"}
	taskType := models.TaskType{Name: "busy_resume_type", DisplayName: "忙碌恢复测试", EngineMode: "debate_full"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusAnalyzing}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	execLog := models.TaskExecutionLog{
		RepoID:       repo.ID,
		TaskTypeID:   taskType.ID,
		TaskReportID: &report.ID,
		Status:       models.StatusAnalyzing,
		StartTime:    time.Now(),
	}
	if err := db.Create(&execLog).Error; err != nil {
		t.Fatalf("create execution log: %v", err)
	}

	if err := EnqueueResumeTask(report); !errors.Is(err, ErrResumeBusyReport) {
		t.Fatalf("EnqueueResumeTask() error = %v, want %v", err, ErrResumeBusyReport)
	}

	var unchangedReport models.TaskReport
	if err := db.First(&unchangedReport, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if unchangedReport.Status != models.StatusAnalyzing {
		t.Fatalf("status = %q, want %q", unchangedReport.Status, models.StatusAnalyzing)
	}
	var unchangedLog models.TaskExecutionLog
	if err := db.First(&unchangedLog, execLog.ID).Error; err != nil {
		t.Fatalf("load execution log: %v", err)
	}
	if unchangedLog.Status != models.StatusAnalyzing || unchangedLog.IsResume {
		t.Fatalf("log changed: status=%q resume=%t", unchangedLog.Status, unchangedLog.IsResume)
	}
}

func TestEnqueueResumeTaskRejectsActiveExecutionLog(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue_resume_active_log",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.TaskExecutionLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "active-log-resume-repo", URL: "https://example.com/active-log.git"}
	taskType := models.TaskType{Name: "active_log_resume_type", DisplayName: "活动日志恢复测试", EngineMode: "debate_full"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusQueued}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	execLog := models.TaskExecutionLog{
		RepoID:       repo.ID,
		TaskTypeID:   taskType.ID,
		TaskReportID: &report.ID,
		Status:       models.StatusRunning,
		StartTime:    time.Now(),
	}
	if err := db.Create(&execLog).Error; err != nil {
		t.Fatalf("create execution log: %v", err)
	}

	if err := EnqueueResumeTask(report); !errors.Is(err, ErrResumeActiveLog) {
		t.Fatalf("EnqueueResumeTask() error = %v, want %v", err, ErrResumeActiveLog)
	}

	var unchangedReport models.TaskReport
	if err := db.First(&unchangedReport, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if unchangedReport.Status != models.StatusQueued {
		t.Fatalf("report status = %q, want %q", unchangedReport.Status, models.StatusQueued)
	}
	var unchangedLog models.TaskExecutionLog
	if err := db.First(&unchangedLog, execLog.ID).Error; err != nil {
		t.Fatalf("load execution log: %v", err)
	}
	if unchangedLog.Status != models.StatusRunning || unchangedLog.IsResume {
		t.Fatalf("log changed: status=%q resume=%t", unchangedLog.Status, unchangedLog.IsResume)
	}
}

func TestQueue_ResumeBroadcastsNotify(t *testing.T) {
	// 模拟 StartWorkerPool(3)：设置池规模并扩容唤醒 channel
	workerCount = 3
	workerNotifyChan = make(chan struct{}, 3)
	defer func() {
		workerCount = 0
		workerNotifyChan = make(chan struct{}, 1)
	}()

	SetQueuePaused(true)
	// 清空可能残留的唤醒信号，确保断言精确
	for len(workerNotifyChan) > 0 {
		<-workerNotifyChan
	}

	// 恢复派发时必须为每个 Worker 发送一个唤醒信号
	SetQueuePaused(false)
	if got := len(workerNotifyChan); got != 3 {
		t.Fatalf("expected 3 notify signals after resume, got %d", got)
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
func TestQueue_ResumeTaskFlow(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_queue",
		&models.Department{},
		&models.User{},
		&models.TaskType{},
		&models.Repository{},
		&models.ScheduleConfig{},
		&models.TaskTriggerLog{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
	)
	if db == nil {
		return
	}

	models.DB = db

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
		Status:      models.StatusAnalyzing,
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
