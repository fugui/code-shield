package services

import (
	"code-shield/models"
	"encoding/json"
	"log"
	"reflect"
	"sync"
	"time"

	"gorm.io/datatypes"
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
	// Register hook for coredump risk analysis
	RegisterTaskHook("coredump_risk", handleCampaignHook[models.CoredumpFinding])
	// Register hook for python float comparison scan
	RegisterTaskHook("float_comparison", handleCampaignHook[models.FloatFinding])
	// Register hook for thread creation analysis
	RegisterTaskHook("thread_create", handleCampaignHook[models.ThreadFinding])
	// Register hook for cjson memory leak scan
	RegisterTaskHook("cjson_scan", handleCampaignHook[models.CjsonFinding])
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

// CopyFindingFields 动态拷贝 models.AnalysisFinding 到泛型结构体指针中
func CopyFindingFields(src *models.AnalysisFinding, dst interface{}) {
	sVal := reflect.ValueOf(src).Elem()
	dVal := reflect.ValueOf(dst).Elem()

	for i := 0; i < sVal.NumField(); i++ {
		sField := sVal.Type().Field(i)
		fieldName := sField.Name
		dField := dVal.FieldByName(fieldName)
		if dField.IsValid() && dField.CanSet() && dField.Type() == sVal.Field(i).Type() {
			dField.Set(sVal.Field(i))
		}
	}
}

// SetFieldValue 动态设置结构体中某个字段的值
func SetFieldValue(obj interface{}, fieldName string, val interface{}) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(val))
	}
}

// GetFieldValue 动态获取结构体中某个字段的值
func GetFieldValue(obj interface{}, fieldName string) interface{} {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() {
		return f.Interface()
	}
	return nil
}

// handleCampaignHook 泛型化的专项缺陷处理器
func handleCampaignHook[T any](ctx *taskContext, findings []models.AnalysisFinding) error {
	log.Printf("[TaskHooks] Processing campaign hook for Task: %s, Repo ID: %d, findings count: %d", ctx.taskType.Name, ctx.repo.ID, len(findings))

	for _, f := range findings {
		var cf T
		err := models.DB.Model(new(T)).
			Where("repo_id = ? AND file_path = ? AND line_number = ? AND title = ?", ctx.repo.ID, f.FilePath, f.LineNumber, f.Title).
			First(&cf).Error

		targetStatus := "open"
		if f.Severity == "合格" {
			targetStatus = "closed"
		}

		if err != nil {
			// 1. Create a new campaign finding
			statusLog := []map[string]interface{}{
				{
					"status": targetStatus,
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Initial scan discovery",
				},
			}
			logBytes, _ := json.Marshal(statusLog)

			var newFinding T
			CopyFindingFields(&f, &newFinding)
			SetFieldValue(&newFinding, "RepoID", ctx.repo.ID)
			SetFieldValue(&newFinding, "TaskReportID", ctx.report.ID)
			SetFieldValue(&newFinding, "Status", targetStatus)
			SetFieldValue(&newFinding, "StatusLog", datatypes.JSON(logBytes))

			if err := models.DB.Create(&newFinding).Error; err != nil {
				log.Printf("[TaskHooks] Failed to create campaign finding record: %v", err)
			}
		} else {
			// 2. Existing finding, check workflow status changes
			updatedStatus := GetFieldValue(&cf, "Status").(string)
			var existingLog []map[string]interface{}
			logBytesVal := GetFieldValue(&cf, "StatusLog")
			if logBytesVal != nil {
				if bytes, ok := logBytesVal.([]byte); ok && len(bytes) > 0 {
					_ = json.Unmarshal(bytes, &existingLog)
				} else if datatypesJson, ok := logBytesVal.(datatypes.JSON); ok && len(datatypesJson) > 0 {
					_ = json.Unmarshal(datatypesJson, &existingLog)
				}
			}

			if updatedStatus == "closed" && targetStatus == "open" {
				updatedStatus = "open"
				existingLog = append(existingLog, map[string]interface{}{
					"status": "open",
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Reopened by subsequent scan finding defects",
				})
			} else if updatedStatus != "closed" && targetStatus == "closed" {
				updatedStatus = "closed"
				existingLog = append(existingLog, map[string]interface{}{
					"status": "closed",
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Automatically closed (resolved to合格 by scan)",
				})
			}
			newLogBytes, _ := json.Marshal(existingLog)

			CopyFindingFields(&f, &cf)
			SetFieldValue(&cf, "TaskReportID", ctx.report.ID)
			SetFieldValue(&cf, "Status", updatedStatus)
			SetFieldValue(&cf, "StatusLog", datatypes.JSON(newLogBytes))

			if err := models.DB.Save(&cf).Error; err != nil {
				log.Printf("[TaskHooks] Failed to update campaign finding record: %v", err)
			}
		}
	}

	// 3. Obsolete clean-up
	if err := models.DB.Model(new(T)).Where("repo_id = ? AND task_report_id < ?", ctx.repo.ID, ctx.report.ID).Delete(new(T)).Error; err != nil {
		log.Printf("[TaskHooks] Failed to delete obsolete campaign findings: %v", err)
	}

	return nil
}
