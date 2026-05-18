package services

import (
	"code-shield/models"
	"encoding/json"
	"errors"
	"log"
	"time"
)

// ErrSkipped is returned when a task is skipped due to precondition (e.g., no recent commits).
var ErrSkipped = errors.New("task skipped by precondition")

type Task struct {
	RepoID     uint
	ReportID   uint
	RepoURL    string
	TaskTypeID uint
	AutoNotify bool
	LogID      uint             // ID of TaskExecutionLog
	RunParams  models.RunParams // 运行时参数（从 ScheduleConfig 传入）
}

var TaskQueue = make(chan Task, 300)

// StartWorkerPool starts the background workers
func StartWorkerPool(workers int) {
	log.Printf("[WorkerPool] Starting %d background workers\n", workers)
	for i := 1; i <= workers; i++ {
		go worker(i)
	}
}

// EnqueueTask adds a new task to the queue and creates a pending TaskExecutionLog
func EnqueueTask(scheduleID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string, runParams models.RunParams) {
	// 1. Create a pending execution log
	execLog := models.TaskExecutionLog{
		ScheduleID:  scheduleID,
		RepoID:      repoID,
		TaskTypeID:  taskTypeID,
		TriggerType: triggerType,
		Status:      models.StatusPending,
		StartTime:   time.Now(),
	}

	if err := models.DB.Create(&execLog).Error; err != nil {
		log.Printf("[WorkerPool] Failed to create TaskExecutionLog for Repo %d: %v\n", repoID, err)
		return
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
		return
	}

	task := Task{
		RepoID:     repoID,
		ReportID:   report.ID,
		RepoURL:    repoURL,
		TaskTypeID: taskTypeID,
		AutoNotify: autoNotify,
		LogID:      execLog.ID,
		RunParams:  runParams,
	}

	// Link the execution log to its task report
	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Update("task_report_id", report.ID)

	select {
	case TaskQueue <- task:
		log.Printf("[WorkerPool] Enqueued Repo %d (TaskType %d). Queue size: %d\n", repoID, taskTypeID, len(TaskQueue))
	default:
		log.Printf("[WorkerPool] Queue is full! Dropping task for Repo %d\n", repoID)
		UpdateTaskExecutionLog(execLog.ID, models.StatusFailed, "Queue is full")
	}
}

func worker(id int) {
	for task := range TaskQueue {
		log.Printf("[Worker %d] Picked up task for Repo %d (TaskType %d, LogID: %d)\n", id, task.RepoID, task.TaskTypeID, task.LogID)

		// Update status to running (initial phase)
		UpdateTaskExecutionLog(task.LogID, "running", "")
		// Note: Detailed report status is updated inside RunTaskSync (cloning, analyzing, etc.)

		err := RunTaskSync(task.ReportID, task.RepoURL, task.TaskTypeID, task.AutoNotify, task.RunParams)

		now := time.Now()
		if errors.Is(err, ErrSkipped) {
			log.Printf("[Worker %d] Skipping Repo %d — precondition not met.\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":        models.StatusSkipped,
				"error_message": "前置条件未满足，跳过执行",
				"end_time":      &now,
			})
		} else if err != nil {
			log.Printf("[Worker %d] Task failed for Repo %d: %v\n", id, task.RepoID, err)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":        models.StatusFailed,
				"error_message": err.Error(),
				"end_time":      &now,
			})
		} else {
			log.Printf("[Worker %d] Task completed for Repo %d\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":   models.StatusSuccess,
				"end_time": &now,
			})
		}
	}
}

func UpdateTaskExecutionLog(logID uint, status string, errMsg string) {
	now := time.Now()
	updates := map[string]interface{}{
		"status": status,
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}
	if status == models.StatusSuccess || status == models.StatusFailed {
		updates["end_time"] = &now
	}

	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", logID).Updates(updates)
}

// RecoverPendingTasks 在进程启动时调用，扫描数据库中未完成的任务并重新入队。
// 恢复条件：TaskReport 状态不是终态（success/failed/skipped）。
// 对于执行到一半（例如 cloning, analyzing, post_processing 等）的任务，会先清理已写入的 AnalysisFindings，
// 再重置状态重新执行，避免重复数据。
func RecoverPendingTasks() {
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
		log.Println("[Recovery] No pending task reports found, nothing to recover.")
		return
	}

	log.Printf("[Recovery] Found %d stale task report(s) to evaluate.\n", len(staleReports))

	recovered := 0
	skipped := 0
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
				skipped++
				continue
			}
		}

		// 3. 清理已执行到一半（非排队、非就绪）任务的脏 findings 数据
		if report.Status != models.StatusQueued && report.Status != models.StatusPending {
			result := models.DB.Where("task_report_id = ?", report.ID).Delete(&models.AnalysisFinding{})
			if result.Error != nil {
				log.Printf("[Recovery] ReportID %d: failed to clean partial findings: %v\n", report.ID, result.Error)
			} else if result.RowsAffected > 0 {
				log.Printf("[Recovery] ReportID %d: cleaned %d partial findings.\n", report.ID, result.RowsAffected)
			}
		}

		// 4. 重置 Report 和 Log 的状态
		models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
			"status":       models.StatusQueued,
			"clone_status": models.StatusPending,
		})
		models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":        models.StatusPending,
			"error_message": "",
			"end_time":      nil,
		})

		// 5. 从 ScheduleConfig 中恢复 RunParams（若有）
		var runParams models.RunParams
		if execLog.Schedule != nil && len(execLog.Schedule.RunParams) > 0 {
			if err := json.Unmarshal(execLog.Schedule.RunParams, &runParams); err != nil {
				log.Printf("[Recovery] ReportID %d: failed to unmarshal RunParams: %v, using defaults.\n", report.ID, err)
			}
		}

		task := Task{
			RepoID:     report.RepoID,
			ReportID:   report.ID,
			RepoURL:    report.Repo.URL,
			TaskTypeID: report.TaskTypeID,
			AutoNotify: execLog.Schedule != nil && execLog.Schedule.AutoNotify,
			LogID:      execLog.ID,
			RunParams:  runParams,
		}

		// 6. 重新入队
		select {
		case TaskQueue <- task:
			log.Printf("[Recovery] Re-queued ReportID %d (Repo: %s, TaskType: %s, LogID: %d).\n",
				report.ID, report.Repo.URL, report.TaskType.Name, execLog.ID)
			recovered++
		default:
			log.Printf("[Recovery] Queue is full! Could not re-queue ReportID %d.\n", report.ID)
			UpdateTaskExecutionLog(execLog.ID, models.StatusFailed, "进程恢复时队列已满，无法重新入队")
			skipped++
		}
	}

	log.Printf("[Recovery] Done: %d task(s) recovered, %d skipped.\n", recovered, skipped)
}
