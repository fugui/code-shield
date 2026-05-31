package services

import (
	"code-shield/models"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// TaskHook is a callback function run when a task finishes successfully
type TaskHook func(ctx *taskContext, findings []models.AnalysisFinding) error

var (
	taskHooksMu sync.RWMutex
	taskHooks   = make(map[string][]TaskHook)
)

// RegisterTaskHook registers a postprocess hook for a specific task type name
func RegisterTaskHook(taskTypeName string, hook TaskHook) {
	taskHooksMu.Lock()
	defer taskHooksMu.Unlock()
	taskHooks[taskTypeName] = append(taskHooks[taskTypeName], hook)
}

// executeHooks runs all hooks registered for the current task type
func (ctx *taskContext) executeHooks(findings []models.AnalysisFinding) {
	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.taskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d hooks for task type %q (Report ID: %d)", len(hooks), ctx.taskType.Name, ctx.report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.taskType.Name, err)
		}
	}
}

func init() {
	// Register hook for test case effectiveness
	RegisterTaskHook("ut_effectiveness", handleUTEffectivenessHook)
}

func handleUTEffectivenessHook(ctx *taskContext, findings []models.AnalysisFinding) error {
	log.Printf("[TaskHooks] Processing ut_effectiveness hook for Repo ID: %d, findings count: %d", ctx.repo.ID, len(findings))

	for _, f := range findings {
		var tf models.TestCaseFinding
		err := models.DB.Where("repo_id = ? AND file_path = ? AND test_case_name = ?", ctx.repo.ID, f.FilePath, f.Title).First(&tf).Error

		targetStatus := "open"
		if f.Severity == "合格" {
			targetStatus = "closed"
		}

		if err != nil {
			// 1. Create a new test case finding
			statusLog := []map[string]interface{}{
				{
					"status": targetStatus,
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Initial scan discovery",
				},
			}
			logBytes, _ := json.Marshal(statusLog)

			tf = models.TestCaseFinding{
				RepoID:       ctx.repo.ID,
				TaskReportID: ctx.report.ID,
				FilePath:     f.FilePath,
				LineNumber:   f.LineNumber,
				TestCaseName: f.Title, // Map scan Title to TestCaseName
				Detail:       f.Detail,
				Severity:     f.Severity,
				Category:     f.Category,
				CodeSnippet:  f.CodeSnippet,
				Suggestion:   f.Suggestion,
				Status:       targetStatus,
				StatusLog:    logBytes,
			}
			if err := models.DB.Create(&tf).Error; err != nil {
				log.Printf("[TaskHooks] Failed to create TestCaseFinding record: %v", err)
			}
		} else {
			// 2. Existing test case, check workflow status changes
			updatedStatus := tf.Status
			var existingLog []map[string]interface{}
			if len(tf.StatusLog) > 0 {
				_ = json.Unmarshal(tf.StatusLog, &existingLog)
			}

			if tf.Status == "closed" && targetStatus == "open" {
				// Reopen if it was closed but now is failing
				updatedStatus = "open"
				existingLog = append(existingLog, map[string]interface{}{
					"status": "open",
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Reopened by subsequent scan finding defects",
				})
			} else if tf.Status != "closed" && targetStatus == "closed" {
				// Auto close if it is now healthy
				updatedStatus = "closed"
				existingLog = append(existingLog, map[string]interface{}{
					"status": "closed",
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Automatically closed (resolved to合格 by scan)",
				})
			}
			logBytes, _ := json.Marshal(existingLog)

			// Update details
			tf.TaskReportID = ctx.report.ID
			tf.LineNumber = f.LineNumber
			tf.Detail = f.Detail
			tf.Severity = f.Severity
			tf.Category = f.Category
			tf.CodeSnippet = f.CodeSnippet
			tf.Suggestion = f.Suggestion
			tf.Status = updatedStatus
			tf.StatusLog = logBytes

			if err := models.DB.Save(&tf).Error; err != nil {
				log.Printf("[TaskHooks] Failed to update TestCaseFinding record: %v", err)
			}
		}
	}

	// 3. Obsolete clean-up: delete test cases that belong to this repo but were not scanned/updated in the current scan
	if err := models.DB.Where("repo_id = ? AND task_report_id < ?", ctx.repo.ID, ctx.report.ID).Delete(&models.TestCaseFinding{}).Error; err != nil {
		log.Printf("[TaskHooks] Failed to delete obsolete TestCaseFinding records: %v", err)
	}

	return nil
}
