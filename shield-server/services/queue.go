package services

import (
	"code-shield/models"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	IsResume   bool             // true 时 worker 调用 ResumeFailedChunks 而非 RunTaskSync
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

// EnqueueResumeTask 将恢复任务放入队列排队执行，而非直接执行。
func EnqueueResumeTask(report models.TaskReport) error {
	// 更新状态为 queued，表示排队等待执行
	models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Update("status", models.StatusQueued)

	task := Task{
		RepoID:   report.RepoID,
		ReportID: report.ID,
		IsResume: true,
	}

	select {
	case TaskQueue <- task:
		log.Printf("[WorkerPool] Enqueued RESUME task for ReportID %d. Queue size: %d\n", report.ID, len(TaskQueue))
		return nil
	default:
		models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Update("status", models.StatusFailed)
		return fmt.Errorf("队列已满，无法入队")
	}
}

func worker(id int) {
	for task := range TaskQueue {
		if task.IsResume {
			log.Printf("[Worker %d] Picked up RESUME task for ReportID %d (Repo %d)\n", id, task.ReportID, task.RepoID)
		} else {
			log.Printf("[Worker %d] Picked up task for Repo %d (TaskType %d, LogID: %d)\n", id, task.RepoID, task.TaskTypeID, task.LogID)
		}

		// Update status to running (initial phase)
		if task.LogID > 0 {
			UpdateTaskExecutionLog(task.LogID, "running", "")
		}

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
	if status == "running" {
		updates["start_time"] = now
	}
	if status == models.StatusSuccess || status == models.StatusFailed {
		updates["end_time"] = &now
	}

	models.DB.Model(&models.TaskExecutionLog{}).Where("id = ?", logID).Updates(updates)
}

// RecoverPendingTasks 在进程启动时调用，扫描数据库中未完成的任务，并根据指定行为进行恢复、忽略或删除。
// action: "recover" (重新入队), "ignore" (忽略), "delete" (从 DB 中彻底物理删除)
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

			// 删除关联的分析发现 (findings)
			models.DB.Where("task_report_id = ?", report.ID).Delete(&models.AnalysisFinding{})
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

		// 3. 清理已执行到一半（非排队、非就绪）任务的脏 findings 数据和物理磁盘报告文件
		if report.Status != models.StatusQueued && report.Status != models.StatusPending {
			// 清除数据库中的 findings
			result := models.DB.Where("task_report_id = ?", report.ID).Delete(&models.AnalysisFinding{})
			if result.Error != nil {
				log.Printf("[Recovery] ReportID %d: failed to clean partial findings: %v\n", report.ID, result.Error)
			} else if result.RowsAffected > 0 {
				log.Printf("[Recovery] ReportID %d: cleaned %d partial findings.\n", report.ID, result.RowsAffected)
			}

			// 清除物理磁盘上的临时文件、分片目录和报告
			CleanReportFiles(report.TaskType.Name, report.ID)
		}

		// 4. 重置 Report 和 Log 的状态，完全清理脏数据和指标信息以确保统计正确
		models.DB.Model(&models.TaskReport{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
			"status":           models.StatusQueued,
			"clone_status":     models.StatusPending,
			"total_chunks":     0,
			"processed_chunks": 0,
			"success_chunks":   0,
			"ai_summary":       "",
			"report_path":      "",
			"score":            0,
			"metrics":          []byte("null"),
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

// CleanReportFiles 递归遍历指定任务目录，物理删除属于特定 reportID 的所有报告、总结、临时输入及分片目录。
func CleanReportFiles(taskTypeName string, reportID uint) {
	reportsBaseDir := filepath.Join(models.AppConfig.Storage.Root, "reports", taskTypeName)
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
