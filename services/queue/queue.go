package queue

import (
	"code-shield/models"
	"code-shield/services/engines/profile"
	"code-shield/services/runner"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func engineConfigHash(raw json.RawMessage) string {
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ErrSkipped 在前置条件未满足时返回，映射自 runner 子包
var ErrSkipped = runner.ErrSkipped

// ErrTaskCanceled 在任务启动前已被取消时返回，映射自 runner 子包
var ErrTaskCanceled = runner.ErrTaskCanceled

// ErrResumeTerminalReport rejects attempts to resurrect an immutable historical report.
var ErrResumeTerminalReport = errors.New("terminal task report cannot be resumed")

// ErrResumeBusyReport rejects attempts to queue a task that is already executing.
var ErrResumeBusyReport = errors.New("active task report cannot be resumed")

// ErrResumeActiveLog rejects attempts to reset an execution log that is still active.
var ErrResumeActiveLog = errors.New("active execution log cannot be resumed")

// workerNotifyChan 用于在新任务入队时即时唤醒空闲 Worker；容量足够覆盖常见动态扩容广播
var workerNotifyChan = make(chan struct{}, workerNotifyCapacity)

// workerCount 记录当前 Worker 池目标规模；调整后无需重启服务即可生效
var workerCount int

// nextWorkerID 生成单调递增的 Worker ID，避免缩容后快速扩容时复用仍在收尾的 ID
var nextWorkerID int

// workerCountLock 保护 workerCount 的读取与热更新
var workerCountLock sync.RWMutex

// workerStops 保存每个活跃 Worker 的独立停止信号；关闭后 Worker 会在当前任务结束后退出
var workerStops = make(map[int]chan struct{})

// workerDones tracks worker goroutine completion so callers can safely wait for
// a replaced worker to stop reading shared worker state.
var workerDones = make(map[int]chan struct{})

// workerNotifyCapacity 为唤醒广播预留容量，避免动态扩容后被 notify 信号占满
const workerNotifyCapacity = 1024

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
		workerCountLock.RLock()
		current := workerCount
		workerCountLock.RUnlock()
		for i := 0; i < current; i++ {
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
	if workers < 1 {
		workers = 1
	}

	workerCountLock.Lock()
	oldStops := make([]chan struct{}, 0, len(workerStops))
	for _, stop := range workerStops {
		oldStops = append(oldStops, stop)
	}

	newStops := make(map[int]chan struct{}, workers)
	for i := 0; i < workers; i++ {
		nextWorkerID++
		newStops[nextWorkerID] = make(chan struct{})
	}
	workerCount = workers
	workerStops = newStops
	workerCountLock.Unlock()

	for _, stop := range oldStops {
		close(stop)
	}

	log.Printf("[WorkerPool] Starting %d background workers (DB-backed persistent queue)\n", workers)
	for id, stop := range newStops {
		startWorker(id, stop)
	}
}

// ResizeWorkerPool 热更新任务 Worker 并发数。
// 扩容时立即启动新 Worker；缩容时不取消当前任务，空闲 Worker 或完成当前任务的 Worker 会自然退出。
func ResizeWorkerPool(workers int) {
	if workers < 1 {
		workers = 1
	}

	workerCountLock.Lock()
	old := workerCount
	if old == workers {
		workerCountLock.Unlock()
		return
	}
	workerCount = workers
	workerCountLock.Unlock()

	if workers > old {
		log.Printf("[WorkerPool] Scaling workers up: %d -> %d\n", old, workers)
		newWorkers := make([]struct {
			id   int
			stop chan struct{}
		}, 0, workers-old)
		for i := old; i < workers; i++ {
			workerCountLock.Lock()
			nextWorkerID++
			id := nextWorkerID
			stop := make(chan struct{})
			workerStops[id] = stop
			workerCountLock.Unlock()
			newWorkers = append(newWorkers, struct {
				id   int
				stop chan struct{}
			}{id: id, stop: stop})
		}
		for _, w := range newWorkers {
			startWorker(w.id, w.stop)
		}
		return
	}

	log.Printf("[WorkerPool] Scaling workers down: %d -> %d; running tasks will continue to completion\n", old, workers)
	workerCountLock.Lock()
	surplus := len(workerStops) - workers
	stopsToClose := make([]chan struct{}, 0, max(surplus, 0))
	for id, stop := range workerStops {
		if len(stopsToClose) >= surplus {
			break
		}
		delete(workerStops, id)
		stopsToClose = append(stopsToClose, stop)
	}
	workerCountLock.Unlock()

	for _, stop := range stopsToClose {
		close(stop)
	}
}

// workerPoolSize 返回当前目标 Worker 数
func workerPoolSize() int {
	workerCountLock.RLock()
	defer workerCountLock.RUnlock()
	return workerCount
}

// WorkerPoolSize 返回当前目标 Worker 数，供调试与监控接口使用
func WorkerPoolSize() int {
	return workerPoolSize()
}

// removeWorker 在 Worker 退出时清理停止信号映射
func removeWorker(id int, stop <-chan struct{}) {
	workerCountLock.Lock()
	if workerStops[id] == stop {
		delete(workerStops, id)
	}
	delete(workerDones, id)
	workerCountLock.Unlock()
}

func startWorker(id int, stop <-chan struct{}) {
	done := make(chan struct{})
	workerCountLock.Lock()
	workerDones[id] = done
	workerCountLock.Unlock()
	go func() {
		defer close(done)
		worker(id, stop)
	}()
}

// EnqueueTask adds a new task to the queue and creates a pending TaskExecutionLog
func EnqueueTask(scheduleID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) {
	EnqueueTaskWithTriggerLog(scheduleID, nil, repoID, repoURL, taskTypeID, autoNotify, triggerType, runParams)
}

// EnqueueTaskWithTriggerLog supports linking a parent TaskTriggerLog and returns true if enqueued successfully
func EnqueueTaskWithTriggerLog(scheduleID *uint, triggerLogID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) bool {
	// 检查队列最大排队上限 (MaxQueueSize，-1 表示不限)
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

	var execLog models.TaskExecutionLog
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		var activeLogCount int64
		if err := tx.Model(&models.TaskExecutionLog{}).
			Where("repo_id = ? AND task_type_id = ? AND status NOT IN ?",
				repoID, taskTypeID, models.TerminalTaskStatuses()).
			Count(&activeLogCount).Error; err != nil {
			return fmt.Errorf("check active execution log: %w", err)
		}
		if activeLogCount > 0 {
			return fmt.Errorf("repo %d task type %d already has an active execution log", repoID, taskTypeID)
		}

		var activeReportCount int64
		if err := tx.Model(&models.TaskReport{}).
			Where("repo_id = ? AND task_type_id = ? AND status NOT IN ?",
				repoID, taskTypeID, models.TerminalTaskStatuses()).
			Count(&activeReportCount).Error; err != nil {
			return fmt.Errorf("check active task report: %w", err)
		}
		if activeReportCount > 0 {
			return fmt.Errorf("repo %d task type %d already has an active task report", repoID, taskTypeID)
		}

		var taskType models.TaskType
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&taskType, taskTypeID).Error; err != nil {
			return fmt.Errorf("load task type: %w", err)
		}
		parsedProfile, scanProfileHash, err := profile.Parse(json.RawMessage(taskType.EngineConfig))
		if err != nil {
			return fmt.Errorf("invalid engine config: %w", err)
		}
		scanProfileRaw, err := json.Marshal(parsedProfile)
		if err != nil {
			return fmt.Errorf("encode scan profile snapshot: %w", err)
		}
		revisionContent, err := buildTaskTypeRevision(taskType)
		if err != nil {
			return fmt.Errorf("build task type revision: %w", err)
		}
		revision, err := ensureTaskTypeRevision(tx, revisionContent)
		if err != nil {
			return fmt.Errorf("ensure task type revision: %w", err)
		}
		if err := tx.Model(&models.TaskType{}).Where("id = ?", taskType.ID).
			Update("current_revision_id", revision.ID).Error; err != nil {
			return fmt.Errorf("bind current task type revision: %w", err)
		}
		executionSnapshot, err := buildExecutionSnapshot(revision, scanProfileRaw, scanProfileHash)
		if err != nil {
			return fmt.Errorf("build execution snapshot: %w", err)
		}

		execLog = models.TaskExecutionLog{
			ScheduleID:     scheduleID,
			TriggerLogID:   triggerLogID,
			RepoID:         repoID,
			TaskTypeID:     taskTypeID,
			TriggerType:    triggerType,
			Status:         models.StatusPending,
			StatusPriority: models.GetStatusPriority(models.StatusPending),
			StartTime:      time.Now(),
		}
		if err := tx.Create(&execLog).Error; err != nil {
			return fmt.Errorf("create execution log: %w", err)
		}

		report := models.TaskReport{
			RepoID:           repoID,
			TaskTypeID:       taskTypeID,
			Status:           models.StatusQueued,
			CloneStatus:      models.StatusPending,
			EngineMode:       taskType.EngineMode,
			ScanProfile:      scanProfileRaw,
			ScanProfileHash:  scanProfileHash,
			PromptVersion:    "v1",
			EngineConfigHash: revision.EngineConfigHash,
			PlannerVersion:   "v1",

			AssessmentConfig:         datatypes.JSON(executionSnapshot.Snapshot.AssessmentConfig),
			AssessmentConfigHash:     executionSnapshot.Snapshot.AssessmentConfigHash,
			PromptContent:            executionSnapshot.Snapshot.PromptContent,
			PromptContentHash:        executionSnapshot.Snapshot.PromptContentHash,
			Categories:               datatypes.JSON(marshalSnapshotProjection(executionSnapshot.Snapshot.Categories)),
			CategorySchemaHash:       executionSnapshot.Snapshot.CategorySchemaHash,
			TaxonomySchemaVersion:    executionSnapshot.Snapshot.TaxonomySchemaVersion,
			TaxonomyHash:             executionSnapshot.Snapshot.TaxonomyHash,
			DomainFamily:             executionSnapshot.Snapshot.DomainFamily,
			DefenseDimensions:        datatypes.JSON(executionSnapshot.Snapshot.DefenseDimensions),
			GovernanceMode:           executionSnapshot.Snapshot.GovernanceMode,
			DomainLabel:              executionSnapshot.Snapshot.DomainLabel,
			TargetSemantics:          datatypes.JSON(executionSnapshot.Snapshot.TargetSemantics),
			DisplaySemantics:         datatypes.JSON(executionSnapshot.Snapshot.DisplaySemantics),
			PostprocessContent:       executionSnapshot.Snapshot.PostprocessContent,
			PostprocessHash:          executionSnapshot.Snapshot.PostprocessHash,
			TaskTypeRevisionID:       &revision.ID,
			ExecutionSnapshot:        datatypes.JSON(executionSnapshot.Encoded),
			ExecutionSnapshotVersion: executionSnapshotVersion,
			ExecutionSnapshotState:   "complete",
		}
		if err := tx.Create(&report).Error; err != nil {
			return fmt.Errorf("create task report: %w", err)
		}
		if err := tx.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).
			Update("task_report_id", report.ID).Error; err != nil {
			return fmt.Errorf("link execution log: %w", err)
		}
		return nil
	})
	if err != nil {
		log.Printf("[WorkerPool] Failed to enqueue Repo %d (TaskType %d): %v\n", repoID, taskTypeID, err)
		return false
	}

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

	var execLog models.TaskExecutionLog
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		var lockedReport models.TaskReport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedReport, report.ID).Error; err != nil {
			return fmt.Errorf("lock task report: %w", err)
		}
		if models.IsTerminalTaskStatus(lockedReport.Status) {
			return fmt.Errorf("%w: report %d status %q", ErrResumeTerminalReport, report.ID, lockedReport.Status)
		}
		if models.IsBusyTaskReportStatus(lockedReport.Status) {
			return fmt.Errorf("%w: report %d status %q", ErrResumeBusyReport, report.ID, lockedReport.Status)
		}

		var lockedLog models.TaskExecutionLog
		logErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_report_id = ?", report.ID).
			First(&lockedLog).Error
		if errors.Is(logErr, gorm.ErrRecordNotFound) {
			// 容错：执行日志丢失时补建一条恢复日志，保证队列链路完整
			lockedLog = models.TaskExecutionLog{
				RepoID:         lockedReport.RepoID,
				TaskReportID:   &lockedReport.ID,
				TaskTypeID:     lockedReport.TaskTypeID,
				TriggerType:    "resume",
				Status:         models.StatusPending,
				StatusPriority: models.GetStatusPriority(models.StatusPending),
				IsResume:       true,
				StartTime:      time.Now(),
			}
			if err := tx.Create(&lockedLog).Error; err != nil {
				return fmt.Errorf("创建恢复任务执行日志失败: %w", err)
			}
		} else if logErr != nil {
			return fmt.Errorf("查询恢复任务执行日志失败: %w", logErr)
		} else if !models.IsTerminalTaskStatus(lockedLog.Status) {
			return fmt.Errorf("%w: log %d status %q", ErrResumeActiveLog, lockedLog.ID, lockedLog.Status)
		} else {
			if err := tx.Model(&models.TaskExecutionLog{}).Where("id = ?", lockedLog.ID).Updates(map[string]interface{}{
				"status":          models.StatusPending,
				"status_priority": models.GetStatusPriority(models.StatusPending),
				"error_message":   "",
				"end_time":        nil,
				"is_resume":       true,
			}).Error; err != nil {
				return fmt.Errorf("重置恢复任务执行日志失败: %w", err)
			}
			lockedLog.Status = models.StatusPending
		}

		result := tx.Model(&models.TaskReport{}).
			Where("id = ? AND status NOT IN ?", lockedReport.ID, models.TerminalTaskStatuses()).
			Update("status", models.StatusQueued)
		if result.Error != nil {
			return fmt.Errorf("更新任务报告状态失败: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: report %d", ErrResumeTerminalReport, lockedReport.ID)
		}

		execLog = lockedLog
		return nil
	})
	if err != nil {
		return err
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

		// Reload the claimed row on the transaction connection. PostgreSQL
		// may route subsequent reads through another pool connection whose
		// snapshot misses the task_report_id link written immediately before
		// the worker was notified.
		if err := tx.
			Preload("Repo").
			Preload("Schedule").
			First(&execLog, execLog.ID).Error; err != nil {
			return err
		}

		found = true
		return nil
	})

	if err != nil || !found {
		return nil, false
	}

	failClaimedTask := func(message string) {
		now := time.Now()
		models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":          models.StatusFailed,
			"status_priority": models.GetStatusPriority(models.StatusFailed),
			"error_message":   message,
			"end_time":        &now,
		})
	}

	// 反查关联的 TaskReport
	if execLog.TaskReportID == nil {
		failClaimedTask(fmt.Sprintf("task report is missing for execution log %d", execLog.ID))
		return nil, false
	}
	var report models.TaskReport
	if err := models.DB.First(&report, *execLog.TaskReportID).Error; err != nil {
		failClaimedTask(fmt.Sprintf("report %d not found: %v", *execLog.TaskReportID, err))
		return nil, false
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

func worker(id int, stop <-chan struct{}) {
	defer removeWorker(id, stop)

	for {
		select {
		case <-stop:
			log.Printf("[Worker %d] Stopped gracefully after completing current work\n", id)
			return
		default:
		}

		task, found := fetchNextPendingTask()
		if !found {
			// 当前无任务，等待新任务通知信号，或每 2 秒自愈轮询
			select {
			case <-stop:
				log.Printf("[Worker %d] Stopped gracefully while idle\n", id)
				return
			case <-workerNotifyChan:
			case <-time.After(2 * time.Second):
			}
			continue
		}

		log.Printf("[Worker %d] Picked up task for Repo %d (TaskType %d, LogID: %d, ReportID: %d)\n",
			id, task.RepoID, task.TaskTypeID, task.LogID, task.ReportID)

		var err error
		if task.IsResume {
			err = runner.ResumeChunkedTask(task.ReportID)
		} else {
			err = runner.RunTaskSync(task.ReportID, task.RepoURL, task.TaskTypeID, task.AutoNotify, task.RunParams)
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
			log.Printf("[Worker %d] Skipping Repo %d — scope planner skipped task.\n", id, task.RepoID)
			models.DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
					"status":          models.StatusSkipped,
					"status_priority": models.GetStatusPriority(models.StatusSkipped),
					"error_message":   "scope planner skipped task",
					"end_time":        &now,
				}).Error; err != nil {
					return err
				}
				return tx.Model(&models.TaskReport{}).Where("id = ? AND status NOT IN ?", task.ReportID, models.TerminalTaskStatuses()).Update("status", models.StatusSkipped).Error
			})
		} else if errors.Is(err, ErrTaskCanceled) {
			log.Printf("[Worker %d] Task canceled before execution for Repo %d (TaskType %d, LogID: %d, ReportID: %d)\n",
				id, task.RepoID, task.TaskTypeID, task.LogID, task.ReportID)
			if task.LogID != 0 {
				models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
					"status":          models.StatusFailed,
					"status_priority": models.GetStatusPriority(models.StatusFailed),
					"error_message":   "任务已在删除前取消",
					"end_time":        &now,
				})
			}
			models.DB.Model(&models.TaskReport{}).
				Where("id = ? AND status NOT IN ?", task.ReportID, models.TerminalTaskStatuses()).
				Updates(map[string]interface{}{"status": models.StatusFailed})
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

// UpdateTaskExecutionLog 更新执行日志状态
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
	terminatedStatuses := models.TerminalTaskStatuses()

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
	type bundleResumeKey struct {
		taskTypeName string
		engineMode   string
	}
	bundleResumeArtifacts := make(map[bundleResumeKey]map[uint]bool)
	for _, report := range staleReports {
		key := bundleResumeKey{taskTypeName: report.TaskType.Name, engineMode: report.TaskType.EngineMode}
		if _, exists := bundleResumeArtifacts[key]; exists {
			continue
		}

		reportIDs := make([]uint, 0, len(staleReports))
		for _, candidate := range staleReports {
			if candidate.TaskType.Name == key.taskTypeName && candidate.TaskType.EngineMode == key.engineMode {
				reportIDs = append(reportIDs, candidate.ID)
			}
		}
		bundleResumeArtifacts[key] = runner.FindBundleResumeArtifacts(key.taskTypeName, key.engineMode, reportIDs)
	}

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

		key := bundleResumeKey{taskTypeName: report.TaskType.Name, engineMode: report.TaskType.EngineMode}
		hasResumeArtifacts := bundleResumeArtifacts[key][report.ID]

		// 3. 清理已执行到一半（非排队、非就绪）任务的物理磁盘报告文件；
		// 存在 bundle checkpoint 时保留产物，交给统一恢复入口复用。
		if report.Status != models.StatusQueued && report.Status != models.StatusPending && !hasResumeArtifacts {
			CleanReportFiles(report.TaskType.Name, report.ID)
		}
		// 4. 重置 Report 和 Log 的状态为 pending / queued；
		// 可恢复任务保留 report_path，避免原 checkpoint 目录在恢复时漂移。
		reportUpdates := map[string]interface{}{
			"status":           models.StatusQueued,
			"clone_status":     models.StatusPending,
			"total_chunks":     0,
			"processed_chunks": 0,
			"success_chunks":   0,
			"ai_summary":       "",
			"score":            0,
			"metrics":          datatypes.JSON("null"),
		}
		if !hasResumeArtifacts {
			reportUpdates["report_path"] = ""
		}
		if _, err := models.UpdateActiveTaskReport(models.DB, report.ID, reportUpdates); err != nil {
			log.Printf("[Recovery] Skip immutable report %d: %v", report.ID, err)
			continue
		}
		models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":          models.StatusPending,
			"status_priority": models.GetStatusPriority(models.StatusPending),
			"error_message":   "",
			"end_time":        nil,
			"is_resume":       hasResumeArtifacts,
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
			(info.IsDir() && (strings.HasPrefix(name, fmt.Sprintf("chunks-%d-", reportID)) ||
				strings.HasPrefix(name, fmt.Sprintf("debate-chunks-%d-", reportID)))) {
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
