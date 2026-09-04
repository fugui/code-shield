package services

import (
	"code-shield/models"
	"code-shield/services/runner"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrSkipped 在前置条件未满足时返回，映射自 runner 子包
var ErrSkipped = runner.ErrSkipped

type Task struct {
	RepoID     uint
	ReportID   uint
	RepoURL    string
	TaskTypeID uint
	AutoNotify bool
	LogID      uint             // ID of TaskExecutionLog
	RunParams  models.RunParams // 运行时参数（从 ScheduleConfig 传入）
	IsResume   bool             // true 时 worker 调用 ResumeFailedChunks 而非 RunTaskSync
}

// workerNotifyChan 用于在新任务入队时即时唤醒空闲 Worker
var workerNotifyChan = make(chan struct{}, 1)

// workerCount 记录当前 Worker 池规模，恢复派发时用于广播唤醒全部 Worker
var workerCount int

// isQueuePaused 内存级原子开关缓存（优雅排空模式/暂停派发）
var isQueuePaused atomic.Bool

// IsQueuePaused 查询当前队列是否处于暂停派发状态
func IsQueuePaused() bool {
	return isQueuePaused.Load()
}

// SetQueuePaused 设置调度开关，并在恢复派发时即时唤醒 Worker
func SetQueuePaused(paused bool) {
	isQueuePaused.Store(paused)
	if !paused {
		// 恢复派发时广播唤醒所有 Worker（每个 Worker 一个信号）
		for i := 0; i < workerCount; i++ {
			NotifyWorker()
		}
	}
}

// InitQueueState 在服务启动时从 DB 加载初始队列状态
func InitQueueState() {
	if models.DB == nil {
		return
	}
	var cfg models.SystemConfig
	if err := models.DB.First(&cfg, 1).Error; err == nil {
		isQueuePaused.Store(cfg.QueuePaused)
		if cfg.QueuePaused {
			log.Println("[WorkerPool] Queue dispatch is currently PAUSED (Drain Mode) based on SystemConfig.")
		}
	}
}

// NotifyWorker 发送唤醒信号
func NotifyWorker() {
	select {
	case workerNotifyChan <- struct{}{}:
	default:
	}
}

// StartWorkerPool starts the background workers
func StartWorkerPool(workers int) {
	workerCount = workers
	// 广播唤醒需要至少能容纳全部 Worker 的信号容量
	capacity := workers
	if capacity < 1 {
		capacity = 1
	}
	workerNotifyChan = make(chan struct{}, capacity)
	log.Printf("[WorkerPool] Starting %d background workers (DB-backed persistent queue)\n", workers)
	for i := 1; i <= workers; i++ {
		go worker(i)
	}
}

// EnqueueTask adds a new task to the queue and creates a pending TaskExecutionLog
func EnqueueTask(scheduleID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) {
	EnqueueTaskWithTriggerLog(scheduleID, nil, repoID, repoURL, taskTypeID, autoNotify, triggerType, runParams)
}

// EnqueueTaskWithTriggerLog supports linking a parent TaskTriggerLog and returns true if enqueued successfully
func EnqueueTaskWithTriggerLog(scheduleID *uint, triggerLogID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) bool {
	// 双重去重保护：检查 ExecutionLog 和 TaskReport，防止用户删除 pending 后 Cron 重入队风暴。
	// 1. 检查执行日志：是否有未完成的执行记录
	var logCount int64
	models.DB.Model(&models.TaskExecutionLog{}).
		Where("repo_id = ? AND task_type_id = ? AND status NOT IN (?, ?, ?)",
			repoID, taskTypeID, models.StatusSuccess, models.StatusFailed, models.StatusSkipped).
		Count(&logCount)
	if logCount > 0 {
		log.Printf("[WorkerPool] Skipped enqueuing Repo %d (TaskType %d) — already has active execution log.\n", repoID, taskTypeID)
		return false
	}

	// 2. 检查任务报告：是否有尚未完成的报告
	var reportCount int64
	models.DB.Model(&models.TaskReport{}).
		Where("repo_id = ? AND task_type_id = ? AND status NOT IN (?, ?, ?)",
			repoID, taskTypeID, models.StatusSuccess, models.StatusFailed, models.StatusSkipped).
		Count(&reportCount)
	if reportCount > 0 {
		log.Printf("[WorkerPool] Skipped enqueuing Repo %d (TaskType %d) — already has active task report.\n", repoID, taskTypeID)
		return false
	}

	// 3. 检查队列最大排队上限 (MaxQueueSize，-1 表示不限)
	if models.AppConfig.Server.MaxQueueSize > 0 {
		var pendingCount int64
		models.DB.Model(&models.TaskExecutionLog{}).
			Where("status = ?", models.StatusPending).
			Count(&pendingCount)
		if int(pendingCount) >= models.AppConfig.Server.MaxQueueSize {
			log.Printf("[WorkerPool] Enqueue rejected: current pending tasks (%d) reached max_queue_size (%d)\n",
				pendingCount, models.AppConfig.Server.MaxQueueSize)
			return false
		}
	}

	// 1. Create a pending execution log
	execLog := models.TaskExecutionLog{
		ScheduleID:     scheduleID,
		TriggerLogID:   triggerLogID,
		RepoID:         repoID,
		TaskTypeID:     taskTypeID,
		TriggerType:    triggerType,
		Status:         models.StatusPending,
		StatusPriority: models.GetStatusPriority(models.StatusPending),
		StartTime:      time.Now(),
	}

	if err := models.DB.Create(&execLog).Error; err != nil {
		log.Printf("[WorkerPool] Failed to create TaskExecutionLog for Repo %d: %v\n", repoID, err)
		return false
	}

	// 2. Create the initial queued TaskReport
	report := models.TaskReport{
		RepoID:      repoID,
		TaskTypeID:  taskTypeID,
		BaseCommit:  "HEAD~1",
		HeadCommit:  "HEAD",
		Status:      models.StatusQueued,
		CloneStatus: models.StatusPending,
	}
	if err := models.DB.Create(&report).Error; err != nil {
		log.Printf("[WorkerPool] Failed to create TaskReport for Repo %d: %v\n", repoID, err)
		return false
	}

	// Link the execution log to its task report
	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Update("task_report_id", report.ID)

	// 触发 Worker 唤醒
	NotifyWorker()
	log.Printf("[WorkerPool] Enqueued Repo %d (TaskType %d, LogID: %d) successfully into database queue.\n",
		repoID, taskTypeID, execLog.ID)
	return true
}

// EnqueueResumeTask 将恢复任务放入队列排队执行，而非直接执行。
func EnqueueResumeTask(report models.TaskReport) error {
	if models.AppConfig.Server.MaxQueueSize > 0 {
		var pendingCount int64
		models.DB.Model(&models.TaskExecutionLog{}).
			Where("status = ?", models.StatusPending).
			Count(&pendingCount)
		if int(pendingCount) >= models.AppConfig.Server.MaxQueueSize {
			return fmt.Errorf("当前排队任务数已达系统上限 (%d)，无法入队", models.AppConfig.Server.MaxQueueSize)
		}
	}

	// 将关联的执行日志置为 pending 并标记为恢复任务，供 Worker 原子抢占
	var execLog models.TaskExecutionLog
	err := models.DB.Where("task_report_id = ?", report.ID).First(&execLog).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 容错：执行日志丢失时补建一条恢复日志，保证队列链路完整
		execLog = models.TaskExecutionLog{
			RepoID:         report.RepoID,
			TaskReportID:   &report.ID,
			TaskTypeID:     report.TaskTypeID,
			TriggerType:    "resume",
			Status:         models.StatusPending,
			StatusPriority: models.GetStatusPriority(models.StatusPending),
			IsResume:       true,
			StartTime:      time.Now(),
		}
		if err := models.DB.Create(&execLog).Error; err != nil {
			return fmt.Errorf("创建恢复任务执行日志失败: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("查询恢复任务执行日志失败: %w", err)
	} else {
		// 复用原执行日志，重置为 pending 并标记恢复
		if err := models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":          models.StatusPending,
			"status_priority": models.GetStatusPriority(models.StatusPending),
			"error_message":   "",
			"end_time":        nil,
			"is_resume":       true,
		}).Error; err != nil {
			return fmt.Errorf("重置恢复任务执行日志失败: %w", err)
		}
	}

	// 更新报告状态为 queued，表示排队等待执行
	if err := models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Update("status", models.StatusQueued).Error; err != nil {
		return fmt.Errorf("更新任务报告状态失败: %w", err)
	}

	NotifyWorker()
	log.Printf("[WorkerPool] Enqueued RESUME task for ReportID %d (LogID: %d)\n", report.ID, execLog.ID)
	return nil
}

// fetchNextPendingTask 从数据库中原子抢占并拉取一条 Pending 状态的任务
func fetchNextPendingTask() (*Task, bool) {
	// 【新增拦截】处于暂停/排空模式时直接返回，Worker 进入休眠等待
	if IsQueuePaused() {
		return nil, false
	}

	if models.DB == nil {
		return nil, false
	}

	var execLog models.TaskExecutionLog
	var found bool

	// 使用数据库事务原子抢占最早的一条 pending 任务
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		// 事务内二次复查暂停开关，缩小与外部暂停操作的竞态窗口
		if IsQueuePaused() {
			return nil
		}

		// 查找最早创建的 pending 记录，使用 FOR UPDATE SKIP LOCKED 确保并发安全
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Preload("Repo").
			Preload("Schedule").
			Where("status = ?", models.StatusPending).
			Order("id ASC")

		if err := query.First(&execLog).Error; err != nil {
			return err
		}

		// 抢占成功，原子更新状态为 running
		now := time.Now()
		if err := tx.Model(&models.TaskExecutionLog{}).
			Where("id = ?", execLog.ID).
			Updates(map[string]interface{}{
				"status":          models.StatusRunning,
				"status_priority": models.GetStatusPriority(models.StatusRunning),
				"start_time":      now,
			}).Error; err != nil {
			return err
		}

		found = true
		return nil
	})

	if err != nil || !found {
		return nil, false
	}

	// 反查关联的 TaskReport
	var report models.TaskReport
	if execLog.TaskReportID != nil {
		models.DB.First(&report, *execLog.TaskReportID)
	}

	var runParams models.RunParams
	if execLog.Schedule != nil && len(execLog.Schedule.RunParams) > 0 {
		_ = json.Unmarshal(execLog.Schedule.RunParams, &runParams)
	}

	return taskFromExecLog(&execLog, report, runParams), true
}

// taskFromExecLog 根据执行日志构造 Worker 任务（IsResume 从日志标记恢复）
func taskFromExecLog(execLog *models.TaskExecutionLog, report models.TaskReport, runParams models.RunParams) *Task {
	return &Task{
		RepoID:     execLog.RepoID,
		ReportID:   report.ID,
		RepoURL:    execLog.Repo.URL,
		TaskTypeID: execLog.TaskTypeID,
		AutoNotify: execLog.Schedule != nil && execLog.Schedule.AutoNotify,
		LogID:      execLog.ID,
		RunParams:  runParams,
		IsResume:   execLog.IsResume,
	}
}

func worker(id int) {
	for {
		task, found := fetchNextPendingTask()
		if !found {
			// 当前无任务，等待新任务通知信号，或每 2 秒自愈轮询
			select {
			case <-workerNotifyChan:
			case <-time.After(2 * time.Second):
			}
			continue
		}

		log.Printf("[Worker %d] Picked up task for Repo %d (TaskType %d, LogID: %d, ReportID: %d)\n",
			id, task.RepoID, task.TaskTypeID, task.LogID, task.ReportID)

		var err error
		if task.IsResume {
			err = ResumeFailedChunks(task.ReportID)
		} else {
			err = RunTaskSync(task.ReportID, task.RepoURL, task.TaskTypeID, task.AutoNotify, task.RunParams)
		}

		now := time.Now()
		if task.LogID == 0 {
			// Resume tasks without a log entry — just log the result
			if err != nil {
				log.Printf("[Worker %d] Resume task failed for ReportID %d: %v\n", id, task.ReportID, err)
			} else {
				log.Printf("[Worker %d] Resume task completed for ReportID %d\n", id, task.ReportID)
			}
		} else if errors.Is(err, ErrSkipped) {
			log.Printf("[Worker %d] Skipping Repo %d — precondition not met.\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":          models.StatusSkipped,
				"status_priority": models.GetStatusPriority(models.StatusSkipped),
				"error_message":   "前置条件未满足，跳过执行",
				"end_time":        &now,
			})
		} else if err != nil {
			log.Printf("[Worker %d] Task failed for Repo %d: %v\n", id, task.RepoID, err)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":          models.StatusFailed,
				"status_priority": models.GetStatusPriority(models.StatusFailed),
				"error_message":   err.Error(),
				"end_time":        &now,
			})
		} else {
			log.Printf("[Worker %d] Task completed for Repo %d\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":          models.StatusSuccess,
				"status_priority": models.GetStatusPriority(models.StatusSuccess),
				"end_time":        &now,
			})
		}
	}
}

func UpdateTaskExecutionLog(logID uint, status string, errMsg string) {
	now := time.Now()
	updates := map[string]interface{}{
		"status":          status,
		"status_priority": models.GetStatusPriority(status),
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}
	if status == "running" {
		updates["start_time"] = now
	}
	if status == models.StatusSuccess || status == models.StatusFailed {
		updates["end_time"] = &now
	}

	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", logID).Updates(updates)
}

// RecoverPendingTasks 在进程启动时调用，扫描数据库中未完成的任务，并根据指定行为进行恢复、忽略或删除。
// action: "recover" (恢复), "ignore" (忽略), "delete" (从 DB 中彻底物理删除)
func RecoverPendingTasks(action string) {
	if action == "ignore" {
		log.Println("[Recovery] Startup stale task action is set to 'ignore'. Skipping stale task processing.")
		return
	}

	var staleReports []models.TaskReport
	terminatedStatuses := []string{models.StatusSuccess, models.StatusFailed, models.StatusSkipped}

	// 1. 查询所有未完成的核心任务报告
	err := models.DB.
		Preload("Repo").
		Preload("TaskType").
		Where("status NOT IN ?", terminatedStatuses).
		Find(&staleReports).Error
	if err != nil {
		log.Printf("[Recovery] Failed to query stale task reports: %v\n", err)
		return
	}

	if len(staleReports) == 0 {
		log.Println("[Recovery] No pending task reports found, nothing to process.")
		return
	}

	log.Printf("[Recovery] Found %d stale task report(s). Action: %s\n", len(staleReports), action)

	if action == "delete" {
		deletedCount := 0
		for _, report := range staleReports {
			// 查询关联的执行日志
			var execLog models.TaskExecutionLog
			models.DB.Where("task_report_id = ?", report.ID).First(&execLog)

			// 删除任务报告
			models.DB.Delete(&models.TaskReport{}, report.ID)
			// 删除执行日志
			if execLog.ID > 0 {
				models.DB.Delete(&models.TaskExecutionLog{}, execLog.ID)
			}
			// 清除物理磁盘上的临时文件和报告
			CleanReportFiles(report.TaskType.Name, report.ID)
			deletedCount++
		}
		log.Printf("[Recovery] Successfully deleted %d stale task(s) and their associated records.\n", deletedCount)
		return
	}

	// 默认恢复 (recover)
	recovered := 0
	for _, report := range staleReports {
		// 2. 反查对应的执行日志
		var execLog models.TaskExecutionLog
		if err := models.DB.Preload("Schedule").Where("task_report_id = ?", report.ID).First(&execLog).Error; err != nil {
			log.Printf("[Recovery] TaskReport %d has no corresponding TaskExecutionLog, creating a fallback one.\n", report.ID)

			// 容错：如果执行日志不见了，自动创建一个系统恢复类型的执行日志，确保 pipeline 完整
			execLog = models.TaskExecutionLog{
				RepoID:       report.RepoID,
				TaskReportID: &report.ID,
				TaskTypeID:   report.TaskTypeID,
				TriggerType:  "recovery",
				Status:       models.StatusPending,
				StartTime:    time.Now(),
			}
			if err := models.DB.Create(&execLog).Error; err != nil {
				log.Printf("[Recovery] Failed to create fallback TaskExecutionLog for Report %d: %v\n", report.ID, err)
				continue
			}
		}

		// 3. 清理已执行到一半（非排队、非就绪）任务的物理磁盘报告文件
		if report.Status != models.StatusQueued && report.Status != models.StatusPending {
			CleanReportFiles(report.TaskType.Name, report.ID)
		}
		// 4. 重置 Report 和 Log 的状态为 pending / queued
		models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
			"status":           models.StatusQueued,
			"clone_status":     models.StatusPending,
			"total_chunks":     0,
			"processed_chunks": 0,
			"success_chunks":   0,
			"ai_summary":       "",
			"report_path":      "",
			"score":            0,
			"metrics":          datatypes.JSON("null"),
		})
		models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":          models.StatusPending,
			"status_priority": models.GetStatusPriority(models.StatusPending),
			"error_message":   "",
			"end_time":        nil,
		})

		recovered++
	}

	// 唤醒 WorkerPool 开始处理
	NotifyWorker()
	log.Printf("[Recovery] Done: %d task(s) restored to pending state in database.\n", recovered)
}

// CleanReportFiles 递归遍历指定任务目录，物理删除属于特定 reportID 的所有报告、总结、临时输入及分片目录。
func CleanReportFiles(taskTypeName string, reportID uint) {
	reportsBaseDir := filepath.Join(models.AppConfig.GetDataDir(), "reports", taskTypeName)
	if _, err := os.Stat(reportsBaseDir); os.IsNotExist(err) {
		return
	}

	log.Printf("[Recovery] Cleaning physical report files for ReportID %d under %s\n", reportID, reportsBaseDir)

	// 遍历 reportsBaseDir 寻找并删除属于此任务报告的所有匹配项
	filepath.Walk(reportsBaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		name := info.Name()
		isTarget := false

		// 校验是否归属该 reportID
		if strings.Contains(name, fmt.Sprintf("report-%d-", reportID)) ||
			strings.Contains(name, fmt.Sprintf("summary-%d-", reportID)) ||
			strings.Contains(name, fmt.Sprintf("synthesis-input-%d.", reportID)) ||
			strings.Contains(name, fmt.Sprintf("chunk-%d-", reportID)) ||
			(info.IsDir() && strings.HasPrefix(name, fmt.Sprintf("chunks-%d-", reportID))) {
			isTarget = true
		}

		if isTarget {
			log.Printf("[Recovery] Deleting: %s\n", path)
			if info.IsDir() {
				os.RemoveAll(path)
				return filepath.SkipDir // 删除了整个目录后跳过进入子项
			} else {
				os.Remove(path)
			}
		}
		return nil
	})
}

// CleanExpiredTempArtifacts 递归遍历数据目录，清理超过 retentionDays 天数的中间临时分片目录（chunks-*/debate-chunks-*）与临时辅助文件
func CleanExpiredTempArtifacts(retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = 7
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	reportsBaseDir := filepath.Join(models.AppConfig.GetDataDir(), "reports")
	tmpBaseDir := filepath.Join(models.AppConfig.GetDataDir(), "tmp")

	log.Printf("[DiskGC] Starting temp artifacts cleanup (cutoff: %s, retention: %d days)...\n",
		cutoff.Format("2006-01-02 15:04:05"), retentionDays)

	cleanedDirs := 0
	cleanedFiles := 0

	// 1. 清理 tmp/ 临时目录
	if _, err := os.Stat(tmpBaseDir); err == nil {
		filepath.Walk(tmpBaseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || path == tmpBaseDir {
				return nil
			}
			if info.ModTime().Before(cutoff) {
				if info.IsDir() {
					os.RemoveAll(path)
					cleanedDirs++
					return filepath.SkipDir
				}
				os.Remove(path)
				cleanedFiles++
			}
			return nil
		})
	}

	// 2. 清理 reports/ 下过期的分片中间目录及 raw 临时文件
	if _, err := os.Stat(reportsBaseDir); err == nil {
		filepath.Walk(reportsBaseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || path == reportsBaseDir {
				return nil
			}

			name := info.Name()

			// 处理分片临时目录 chunks-* 或 debate-chunks-*
			if info.IsDir() && (strings.HasPrefix(name, "chunks-") || strings.HasPrefix(name, "debate-chunks-")) {
				if info.ModTime().Before(cutoff) {
					os.RemoveAll(path)
					cleanedDirs++
					return filepath.SkipDir
				}
			}

			// 处理临时文件：.raw / .fixed.json / .output.txt / .debug.log
			if !info.IsDir() && (strings.HasSuffix(name, ".raw") ||
				strings.HasSuffix(name, ".fixed.json") ||
				strings.HasSuffix(name, ".debug.log")) {
				if info.ModTime().Before(cutoff) {
					os.Remove(path)
					cleanedFiles++
				}
			}

			return nil
		})
	}

	log.Printf("[DiskGC] Cleanup completed: removed %d temp directories and %d temp files.\n",
		cleanedDirs, cleanedFiles)
}
