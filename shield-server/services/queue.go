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
// 恢复条件：TaskExecutionLog.Status 为 "pending" 或 "running"，
// 且关联的 TaskReport 状态不是终态（success/failed/skipped）。
// 对于执行到一半（running）的任务，会先清理已写入的 AnalysisFindings，
// 再重置状态重新执行，避免重复数据。
func RecoverPendingTasks() {
	var staleLogs []models.TaskExecutionLog
	err := models.DB.
		Preload("Repo").
		Preload("TaskType").
		Preload("Schedule").
		Where("status IN ?", []string{models.StatusPending, "running"}).
		Find(&staleLogs).Error
	if err != nil {
		log.Printf("[Recovery] Failed to query stale execution logs: %v\n", err)
		return
	}

	if len(staleLogs) == 0 {
		log.Println("[Recovery] No pending tasks found, nothing to recover.")
		return
	}

	log.Printf("[Recovery] Found %d stale execution log(s) to evaluate.\n", len(staleLogs))

	recovered := 0
	skipped := 0
	for _, execLog := range staleLogs {
		if execLog.TaskReportID == nil {
			// 没有关联的 TaskReport，直接标记为失败
			log.Printf("[Recovery] ExecLog %d has no TaskReportID, marking as failed.\n", execLog.ID)
			UpdateTaskExecutionLog(execLog.ID, models.StatusFailed, "进程重启后未找到关联的任务报告")
			skipped++
			continue
		}

		// 查询关联的 TaskReport，过滤掉已完成的终态
		var report models.TaskReport
		terminatedStatuses := []string{models.StatusSuccess, models.StatusFailed, models.StatusSkipped}
		if err := models.DB.Where("id = ? AND status NOT IN ?", *execLog.TaskReportID, terminatedStatuses).
			First(&report).Error; err != nil {
			// TaskReport 已处于终态，跳过
			log.Printf("[Recovery] ExecLog %d: TaskReport %d already in terminal state, skipping.\n",
				execLog.ID, *execLog.TaskReportID)
			// 同步修正 ExecLog 状态（防止 ExecLog 和 Report 状态不一致）
			var finalReport models.TaskReport
			if models.DB.First(&finalReport, *execLog.TaskReportID).Error == nil {
				UpdateTaskExecutionLog(execLog.ID, finalReport.Status, "")
			}
			skipped++
			continue
		}

		// 若任务执行到一半（running），清理中途产生的不完整 findings，避免重复
		if execLog.Status == "running" {
			result := models.DB.Where("task_report_id = ?", report.ID).Delete(&models.AnalysisFinding{})
			if result.Error != nil {
				log.Printf("[Recovery] ExecLog %d: failed to clean partial findings: %v\n", execLog.ID, result.Error)
			} else {
				log.Printf("[Recovery] ExecLog %d: cleaned %d partial findings for ReportID %d.\n",
					execLog.ID, result.RowsAffected, report.ID)
			}
		}

		// 重置 TaskReport 状态为 queued
		models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
			"status":       models.StatusQueued,
			"clone_status": models.StatusPending,
		})

		// 重置 TaskExecutionLog 状态为 pending
		models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Updates(map[string]interface{}{
			"status":        models.StatusPending,
			"error_message": "",
			"end_time":      nil,
		})

		// 从 ScheduleConfig 中恢复 RunParams（若有）
		var runParams models.RunParams
		if execLog.Schedule != nil && len(execLog.Schedule.RunParams) > 0 {
			if err := json.Unmarshal(execLog.Schedule.RunParams, &runParams); err != nil {
				log.Printf("[Recovery] ExecLog %d: failed to unmarshal RunParams: %v, using defaults.\n",
					execLog.ID, err)
			}
		}

		task := Task{
			RepoID:     execLog.RepoID,
			ReportID:   report.ID,
			RepoURL:    execLog.Repo.URL,
			TaskTypeID: execLog.TaskTypeID,
			AutoNotify: execLog.Schedule != nil && execLog.Schedule.AutoNotify,
			LogID:      execLog.ID,
			RunParams:  runParams,
		}

		select {
		case TaskQueue <- task:
			log.Printf("[Recovery] Re-queued ExecLog %d → ReportID %d (Repo: %s, TaskType: %s).\n",
				execLog.ID, report.ID, execLog.Repo.URL, execLog.TaskType.Name)
			recovered++
		default:
			log.Printf("[Recovery] Queue is full! Could not re-queue ExecLog %d.\n", execLog.ID)
			UpdateTaskExecutionLog(execLog.ID, models.StatusFailed, "进程恢复时队列已满，无法重新入队")
			skipped++
		}
	}

	log.Printf("[Recovery] Done: %d task(s) recovered, %d skipped.\n", recovered, skipped)
}
