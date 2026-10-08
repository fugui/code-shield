package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"

	"github.com/gin-gonic/gin"
)

func setupTerminalResumeTest(t *testing.T, testName string) (*gin.Engine, models.Repository, models.TaskType) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	db := testdb.SetupIsolatedDB(t, testName,
		&models.Repository{},
		&models.TaskType{},
		&models.TaskTypeRevision{},
		&models.TaskReport{},
		&models.TaskExecutionLog{},
		&models.TaskTriggerLog{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}

	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{
		Name: "terminal-resume-repo",
		URL:  "https://example.com/terminal-resume.git",
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("create repository: %v", err)
	}

	taskType := models.TaskType{
		Name:         "terminal_resume_type",
		DisplayName:  "终态恢复测试",
		EngineMode:   "chunked",
		EngineConfig: []byte(`{"scan_profile":{"version":1,"name":"full_review","target_scope":"business"}}`),
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatalf("create task type: %v", err)
	}

	return gin.New(), repo, taskType
}

func TestResumeTaskTriggersRescanForTerminalReport(t *testing.T) {
	router, repo, taskType := setupTerminalResumeTest(t, "shield_handler_terminal_resume_rescan")
	db := models.DB

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSuccess,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create terminal report: %v", err)
	}

	router.POST("/api/tasks/:id/resume", ResumeTask)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/tasks/1/resume", nil))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(body.Message, "已重新发起扫描") {
		t.Fatalf("message = %q, want rescan notice", body.Message)
	}

	var unchanged models.TaskReport
	if err := db.First(&unchanged, report.ID).Error; err != nil {
		t.Fatalf("load original report: %v", err)
	}
	if unchanged.Status != models.StatusSuccess {
		t.Fatalf("original status = %q, want %q", unchanged.Status, models.StatusSuccess)
	}

	var newReports []models.TaskReport
	if err := db.Where("repo_id = ? AND task_type_id = ? AND id <> ?", repo.ID, taskType.ID, report.ID).
		Find(&newReports).Error; err != nil {
		t.Fatalf("load new report: %v", err)
	}
	if len(newReports) != 1 {
		t.Fatalf("new reports = %d, want 1", len(newReports))
	}
	if newReports[0].Status != models.StatusQueued {
		t.Fatalf("new report status = %q, want %q", newReports[0].Status, models.StatusQueued)
	}

	var triggerLog models.TaskTriggerLog
	if err := db.First(&triggerLog).Error; err != nil {
		t.Fatalf("load trigger log: %v", err)
	}
	if triggerLog.TriggerType != "manual_single" || triggerLog.SuccessCount != 1 {
		t.Fatalf("trigger log = %+v, want manual_single success", triggerLog)
	}
}

func TestResumeTaskDoesNotDuplicateActiveRescan(t *testing.T) {
	router, repo, taskType := setupTerminalResumeTest(t, "shield_handler_terminal_resume_active")
	db := models.DB

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusSuccess,
	}
	if err := db.Create(&report).Error; err != nil {
		t.Fatalf("create terminal report: %v", err)
	}

	activeReport := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     models.StatusQueued,
	}
	if err := db.Create(&activeReport).Error; err != nil {
		t.Fatalf("create active report: %v", err)
	}

	router.POST("/api/tasks/:id/resume", ResumeTask)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/tasks/1/resume", nil))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var reportCount int64
	db.Model(&models.TaskReport{}).
		Where("repo_id = ? AND task_type_id = ? AND id NOT IN ?", repo.ID, taskType.ID, []uint{report.ID, activeReport.ID}).
		Count(&reportCount)
	if reportCount != 0 {
		t.Fatalf("additional reports = %d, want 0", reportCount)
	}
}
