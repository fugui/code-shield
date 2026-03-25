package services

import (
	"code-shield/models"
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
	LogID      uint // ID of TaskExecutionLog
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
func EnqueueTask(scheduleID *uint, repoID uint, repoURL string, taskTypeID uint, autoNotify bool, triggerType string) {
	// 1. Create a pending execution log
	execLog := models.TaskExecutionLog{
		ScheduleID:  scheduleID,
		RepoID:      repoID,
		TaskTypeID:  taskTypeID,
		TriggerType: triggerType,
		Status:      "pending",
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
		Status:      "queued",
		CloneStatus: "pending",
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
	}

	// Link the execution log to its task report
	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", execLog.ID).Update("task_report_id", report.ID)

	select {
	case TaskQueue <- task:
		log.Printf("[WorkerPool] Enqueued Repo %d (TaskType %d). Queue size: %d\n", repoID, taskTypeID, len(TaskQueue))
	default:
		log.Printf("[WorkerPool] Queue is full! Dropping task for Repo %d\n", repoID)
		UpdateTaskExecutionLog(execLog.ID, "failed", "Queue is full")
	}
}

func worker(id int) {
	for task := range TaskQueue {
		log.Printf("[Worker %d] Picked up task for Repo %d (TaskType %d, LogID: %d)\n", id, task.RepoID, task.TaskTypeID, task.LogID)

		// Update status to running
		UpdateTaskExecutionLog(task.LogID, "running", "")
		models.DB.Model(&models.TaskReport{}).Where("id = ?", task.ReportID).Update("status", "running")

		err := RunTaskSync(task.ReportID, task.RepoURL, task.TaskTypeID, task.AutoNotify)

		now := time.Now()
		if errors.Is(err, ErrSkipped) {
			log.Printf("[Worker %d] Skipping Repo %d — precondition not met.\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":        "skipped",
				"error_message": "前置条件未满足，跳过执行",
				"end_time":      &now,
			})
		} else if err != nil {
			log.Printf("[Worker %d] Task failed for Repo %d: %v\n", id, task.RepoID, err)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":        "failed",
				"error_message": err.Error(),
				"end_time":      &now,
			})
		} else {
			log.Printf("[Worker %d] Task completed for Repo %d\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":   "success",
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
	if status == "success" || status == "failed" {
		updates["end_time"] = &now
	}

	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", logID).Updates(updates)
}
