package services

import (
	"code-shield/models"
	"errors"
	"log"
	"time"
)

// ErrNoRecentCommits is returned when a repo has no commits in the past 7 days.
// The worker treats this as a skip, not a failure.
var ErrNoRecentCommits = errors.New("no recent commits in the past 7 days")

type ReviewTask struct {
	RepoID     uint
	ReportID   uint
	RepoURL    string
	AutoNotify bool
	LogID      uint // ID of TaskExecutionLog
}

var TaskQueue = make(chan ReviewTask, 300)

// StartWorkerPool starts the background workers
func StartWorkerPool(workers int) {
	log.Printf("[WorkerPool] Starting %d background workers\n", workers)
	for i := 1; i <= workers; i++ {
		go worker(i)
	}
}

// EnqueueReviewTask adds a new task to the queue and creates a pending TaskExecutionLog
func EnqueueReviewTask(scheduleID *uint, repoID uint, repoURL string, autoNotify bool, triggerType string) {
	// 1. Create a pending execution log
	execLog := models.TaskExecutionLog{
		ScheduleID:  scheduleID,
		RepoID:      repoID,
		TriggerType: triggerType,
		Status:      "pending",
		StartTime:   time.Now(),
	}

	if err := models.DB.Create(&execLog).Error; err != nil {
		log.Printf("[WorkerPool] Failed to create TaskExecutionLog for Repo %d: %v\n", repoID, err)
		return
	}

	// 2. Create the initial queued ReviewReport
	report := models.ReviewReport{
		RepoID:      repoID,
		BaseCommit:  "HEAD~1",
		HeadCommit:  "HEAD",
		Status:      "queued",
		CloneStatus: "pending",
	}
	if err := models.DB.Create(&report).Error; err != nil {
		log.Printf("[WorkerPool] Failed to create ReviewReport for Repo %d: %v\n", repoID, err)
		return
	}

	task := ReviewTask{
		RepoID:     repoID,
		ReportID:   report.ID,
		RepoURL:    repoURL,
		AutoNotify: autoNotify,
		LogID:      execLog.ID,
	}

	select {
	case TaskQueue <- task:
		log.Printf("[WorkerPool] Enqueued Repo %d. Queue size: %d\n", repoID, len(TaskQueue))
	default:
		log.Printf("[WorkerPool] Queue is full! Dropping task for Repo %d\n", repoID)
		UpdateTaskExecutionLog(execLog.ID, "failed", "Queue is full")
	}
}

func worker(id int) {
	for task := range TaskQueue {
		log.Printf("[Worker %d] Picked up task for Repo %d (LogID: %d)\n", id, task.RepoID, task.LogID)

		// Update status to running
		UpdateTaskExecutionLog(task.LogID, "running", "")
		models.DB.Model(&models.ReviewReport{}).Where("id = ?", task.ReportID).Update("status", "running")

		// Wait for completion, pass the context or logger if needed. But for now we just wrap RunAIReview
		// We can change RunAIReview to return an error/status string synchronously since it's now called from within our worker pool wrapper.
		// Right now RunAIReview runs completely independently. Let's make it block so the worker is occupied.
		err := RunAIReviewSync(task.ReportID, task.RepoURL, task.AutoNotify)

		now := time.Now()
		if errors.Is(err, ErrNoRecentCommits) {
			log.Printf("[Worker %d] Skipping Repo %d — no commits in the past 7 days.\n", id, task.RepoID)
			models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", task.LogID).Updates(map[string]interface{}{
				"status":        "skipped",
				"error_message": "最近 7 天内无新提交，跳过检视",
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
