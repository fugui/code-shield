package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-shield/models"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	return testdb.SetupIsolatedDB(t, "shield_task_runner")
}

// mustMigrate 在测试库上执行 AutoMigrate，失败时立即终止测试并给出明确错误。
func mustMigrate(t *testing.T, db *gorm.DB, models ...interface{}) {
	t.Helper()
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
}

func TestFixUnescapedQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool // 修复后是否为合法 JSON
	}{
		{
			name:  "already valid JSON",
			input: `{"title": "hello world", "value": 42}`,
			valid: true,
		},
		{
			name:  "unescaped quotes in value",
			input: `{"title": "Use "proper" method", "score": 1}`,
			valid: true,
		},
		{
			name:  "multiple unescaped quotes",
			input: `{"detail": "Call "foo" then "bar" to fix", "severity": "high"}`,
			valid: true,
		},
		{
			name:  "already escaped quotes",
			input: `{"title": "Use \"proper\" method", "score": 1}`,
			valid: true,
		},
		{
			name:  "nested objects valid",
			input: `{"findings": [{"title": "test", "file": "a.go"}]}`,
			valid: true,
		},
		{
			name:  "unescaped in nested",
			input: `{"findings": [{"title": "Use "sync.Mutex" here", "file": "a.go"}]}`,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fixUnescapedQuotes(tt.input)
			if tt.valid && !json.Valid([]byte(result)) {
				t.Errorf("expected valid JSON after fix, got: %s", result)
			}
		})
	}
}

type MockAIInvoker struct {
	FailInvoke bool
}

func (m *MockAIInvoker) Name() string { return "mock_ai" }
func (m *MockAIInvoker) Invoke(req AIRequest) error {
	if m.FailInvoke {
		return fmt.Errorf("simulated invoke error")
	}
	os.WriteFile(req.OutputPath, []byte(`{"findings": [], "summary": "mock summary"}`), 0644)
	return nil
}

func TestChunkedEngineErrorAggregation(t *testing.T) {
	// 1. Initialize isolated DB
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{})

	// 2. Register mock AI invoker
	mockInvoker := &MockAIInvoker{FailInvoke: true}
	RegisterAIInvoker("mock_error_backend", mockInvoker)

	// 3. Setup mock repository and git structure
	tempDir, err := os.MkdirTemp("", "test-chunk-repo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	exec.Command("git", "-C", tempDir, "init").Run()
	os.WriteFile(filepath.Join(tempDir, "file1.go"), []byte("package main"), 0644)
	exec.Command("git", "-C", tempDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", tempDir, "add", ".").Run()
	exec.Command("git", "-C", tempDir, "commit", "-m", "init").Run()

	// 4. Setup mock models in DB
	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "test-dept-err-agg-" + ts}
	if err := testDB.Create(&dept).Error; err != nil {
		t.Fatalf("failed to create department: %v", err)
	}
	defer testDB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-user-err-agg-" + ts, Email: "test-err-agg-" + ts + "@test.com"}
	if err := testDB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer testDB.Delete(&models.User{}, user.ID)

	taskType := models.TaskType{
		Name:        "code_review_error_agg_" + ts,
		DisplayName: "代码检视",
		EngineMode:  "chunked",
	}
	if err := testDB.Create(&taskType).Error; err != nil {
		t.Fatalf("failed to create task type: %v", err)
	}
	defer testDB.Delete(&models.TaskType{}, taskType.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-repo-error-agg-" + ts,
		URL:          "https://github.com/test/test-repo-error-agg",
	}
	if err := testDB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer testDB.Delete(&models.Repository{}, repo.ID)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "running",
	}
	if err := models.DB.Create(&report).Error; err != nil {
		t.Fatalf("failed to create report: %v", err)
	}
	defer testDB.Delete(&models.TaskReport{}, report.ID)

	// 5. Setup context
	reportPath := filepath.Join(tempDir, "report.md")
	backend := "mock_error_backend"
	ctx := &taskContext{
		ctx:        context.Background(),
		report:     report,
		taskType:   taskType,
		repo:       repo,
		codesPath:  tempDir,
		reportPath: reportPath,
		jsonPath:   filepath.Join(tempDir, "report.json"),
		runParams: models.RunParams{
			AIBackend: &backend,
		},
	}

	// 6. Run chunked engine
	engine := &ChunkedEngine{}
	runErr := engine.Run(ctx)
	if runErr == nil {
		t.Fatal("expected error from ChunkedEngine.Run, got nil")
	}

	if !strings.Contains(runErr.Error(), "simulated invoke error") {
		t.Errorf("expected error message to contain 'simulated invoke error', got: %v", runErr)
	}

	// 7. Verify reportPath + ".output.txt" is created and contains the error
	outputPath := reportPath + ".output.txt"
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("expected output.txt to be created, but it does not exist")
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output.txt: %v", err)
	}

	if !strings.Contains(string(content), "[Code-Shield Error] AI execution failed:") {
		t.Errorf("expected output.txt to contain the error prefix, got: %s", string(content))
	}
	if !strings.Contains(string(content), "simulated invoke error") {
		t.Errorf("expected output.txt to contain the error message, got: %s", string(content))
	}

	// 8. Verify report.json (ChunkExecutionReport) is created and contains correct failure metrics
	if _, err := os.Stat(ctx.jsonPath); os.IsNotExist(err) {
		t.Fatal("expected report.json to be created, but it does not exist")
	}

	reportBytes, err := os.ReadFile(ctx.jsonPath)
	if err != nil {
		t.Fatalf("failed to read report.json: %v", err)
	}

	var execReport TaskSummaryReport
	if err := json.Unmarshal(reportBytes, &execReport); err != nil {
		t.Fatalf("failed to unmarshal report.json: %v", err)
	}

	if execReport.Analysis.TotalChunks != 1 {
		t.Errorf("expected 1 total chunk, got %d", execReport.Analysis.TotalChunks)
	}
	if execReport.Analysis.FailedChunks != 1 {
		t.Errorf("expected 1 failed chunk, got %d", execReport.Analysis.FailedChunks)
	}
	if execReport.Analysis.SuccessChunks != 0 {
		t.Errorf("expected 0 successful chunks, got %d", execReport.Analysis.SuccessChunks)
	}
	if len(execReport.Analysis.Chunks) != 1 {
		t.Fatalf("expected 1 chunk detail entry, got %d", len(execReport.Analysis.Chunks))
	}
	if execReport.Analysis.Chunks[0].Status != "failed" {
		t.Errorf("expected chunk status to be 'failed', got: %s", execReport.Analysis.Chunks[0].Status)
	}
	if execReport.Analysis.Chunks[0].Attempts != 4 { // 1 initial + 3 retries = 4
		t.Errorf("expected 4 attempts, got %d", execReport.Analysis.Chunks[0].Attempts)
	}
	if execReport.Analysis.Chunks[0].Retries != 0 { // 初始执行失败，恢复轮数应为 0
		t.Errorf("expected 0 retries (recovery sessions), got %d", execReport.Analysis.Chunks[0].Retries)
	}
	if !strings.Contains(execReport.Analysis.Chunks[0].ErrorMessage, "simulated invoke error") {
		t.Errorf("expected chunk error message to contain 'simulated invoke error', got: %s", execReport.Analysis.Chunks[0].ErrorMessage)
	}
}

func TestTaskRunnerEarlyFailureLogging(t *testing.T) {
	// 1. Initialize DB
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{})

	// 2. Setup mock models in DB
	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "test-dept-early-fail-" + ts}
	if err := models.DB.Create(&dept).Error; err != nil {
		t.Fatalf("failed to create department: %v", err)
	}
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-user-early-fail-" + ts, Email: "test-early-fail-" + ts + "@test.com"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-early-fail-repo-" + ts,
		URL:          "https://invalid-url-for-test.git",
	}
	if err := models.DB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:        "code_review_early_fail_" + ts,
		DisplayName: "代码检视",
		EngineMode:  "single",
	}
	if err := models.DB.Create(&taskType).Error; err != nil {
		t.Fatalf("failed to create task type: %v", err)
	}
	defer models.DB.Delete(&models.TaskType{}, taskType.ID)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "running",
	}
	if err := models.DB.Create(&report).Error; err != nil {
		t.Fatalf("failed to create report: %v", err)
	}
	defer models.DB.Delete(&models.TaskReport{}, report.ID)

	// 3. RunTaskSync synchronously. Because repo URL is invalid, git pull/clone will fail.
	// But before it fails, prepareOutputPaths should be called, and markFailed should write the failure to output.txt.
	runErr := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{})
	if runErr == nil {
		t.Fatal("expected error from RunTaskSync due to invalid URL, got nil")
	}

	// 4. Fetch the report from database to verify its status is failed and reportPath is set
	var updatedReport models.TaskReport
	models.DB.First(&updatedReport, report.ID)
	if updatedReport.Status != "failed" {
		t.Errorf("expected report status to be 'failed', got: %s", updatedReport.Status)
	}

	if updatedReport.ReportPath == "" {
		t.Fatal("expected report path to be set even for early failure, but it is empty")
	}

	// 5. Verify that output.txt contains the git operation failure error message
	outputPath := updatedReport.GetAbsReportPath() + ".output.txt"
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("expected output.txt to be created for early failure, but it does not exist")
	}
	defer os.RemoveAll(filepath.Dir(updatedReport.GetAbsReportPath())) // Cleanup report directory

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output.txt: %v", err)
	}

	if !strings.Contains(string(content), "[Code-Shield Error] AI execution failed:") {
		t.Errorf("expected output.txt to contain the error prefix, got: %s", string(content))
	}
	if !strings.Contains(string(content), "git operation failed") {
		t.Errorf("expected output.txt to contain the git failure error, got: %s", string(content))
	}
}

func TestPrepareOutputPaths(t *testing.T) {
	// Setup models config
	models.AppConfig.Storage.Root = "/tmp/code-shield-test"

	repo := models.Repository{
		ID:   1,
		Name: "foo/bar",
	}
	taskType := models.TaskType{
		Name: "test_task",
	}

	t.Run("ReportPath is set", func(t *testing.T) {
		createdAt, _ := time.Parse("2006-01-02", "2026-05-20")
		report := models.TaskReport{
			ID:         42,
			ReportPath: "/tmp/code-shield-test/reports/test_task/2026-05-20/report-42-report-foo-bar.md",
			CreatedAt:  createdAt,
		}
		ctx := &taskContext{
			report:   report,
			taskType: taskType,
			repo:     repo,
		}
		ctx.prepareOutputPaths()
		expectedJSON := "/tmp/code-shield-test/reports/test_task/2026-05-20/report-42-summary-foo-bar.json"
		if ctx.reportPath != report.ReportPath {
			t.Errorf("expected reportPath %q, got %q", report.ReportPath, ctx.reportPath)
		}
		if ctx.jsonPath != expectedJSON {
			t.Errorf("expected jsonPath %q, got %q", expectedJSON, ctx.jsonPath)
		}
	})

	t.Run("ReportPath is empty but CreatedAt is set", func(t *testing.T) {
		createdAt, _ := time.Parse("2006-01-02", "2026-05-18")
		report := models.TaskReport{
			ID:        43,
			CreatedAt: createdAt,
		}
		ctx := &taskContext{
			report:   report,
			taskType: taskType,
			repo:     repo,
		}
		ctx.prepareOutputPaths()
		expectedReport := "/tmp/code-shield-test/reports/test_task/2026-05-18/report-43-report-foo-bar.md"
		expectedJSON := "/tmp/code-shield-test/reports/test_task/2026-05-18/report-43-summary-foo-bar.json"
		if ctx.reportPath != expectedReport {
			t.Errorf("expected reportPath %q, got %q", expectedReport, ctx.reportPath)
		}
		if ctx.jsonPath != expectedJSON {
			t.Errorf("expected jsonPath %q, got %q", expectedJSON, ctx.jsonPath)
		}
	})

	t.Run("Both are empty/default", func(t *testing.T) {
		report := models.TaskReport{
			ID: 44,
		}
		ctx := &taskContext{
			report:   report,
			taskType: taskType,
			repo:     repo,
		}
		ctx.prepareOutputPaths()
		today := time.Now().Format("2006-01-02")
		expectedReport := filepath.Join(models.AppConfig.Storage.Root, "reports", taskType.Name, today, "report-44-report-foo-bar.md")
		if ctx.reportPath != expectedReport {
			t.Errorf("expected reportPath %q, got %q", expectedReport, ctx.reportPath)
		}
	})
}

func TestResumeFailedChunksCumulative(t *testing.T) {
	// 1. Initialize DB
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{}, &models.AnalysisFinding{})

	// 2. Register mock success AI invoker
	mockInvoker := &MockAIInvoker{FailInvoke: false}
	RegisterAIInvoker("mock_success_backend", mockInvoker)

	// 3. Setup mock repository and git structure
	tempDir, err := os.MkdirTemp("", "test-resume-repo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repoCodesPath := filepath.Join(tempDir, "git-source")
	os.MkdirAll(repoCodesPath, 0755)

	exec.Command("git", "-C", repoCodesPath, "init").Run()
	os.WriteFile(filepath.Join(repoCodesPath, "file1.go"), []byte("package main"), 0644)
	exec.Command("git", "-C", repoCodesPath, "config", "user.name", "test").Run()
	exec.Command("git", "-C", repoCodesPath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoCodesPath, "add", ".").Run()
	exec.Command("git", "-C", repoCodesPath, "commit", "-m", "init").Run()

	// 4. Setup mock models in DB
	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "test-dept-resume-" + ts}
	if err := models.DB.Create(&dept).Error; err != nil {
		t.Fatalf("failed to create department: %v", err)
	}
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-user-resume-" + ts, Email: "test-resume-" + ts + "@test.com"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-resume-repo-" + ts,
		URL:          repoCodesPath,
	}
	if err := models.DB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:        "code_review_resume_" + ts,
		DisplayName: "代码检视",
		EngineMode:  "chunked",
		AIBackend:   "mock_success_backend",
	}
	if err := models.DB.Create(&taskType).Error; err != nil {
		t.Fatalf("failed to create task type: %v", err)
	}
	defer models.DB.Delete(&models.TaskType{}, taskType.ID)

	// Storage config
	models.AppConfig.Storage.Root = tempDir

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "failed",
	}
	if err := models.DB.Create(&report).Error; err != nil {
		t.Fatalf("failed to create report: %v", err)
	}
	defer models.DB.Delete(&models.TaskReport{}, report.ID)

	// Mock the JSON report path and content
	reportsDir := filepath.Join(tempDir, "reports", taskType.Name, time.Now().Format("2006-01-02"))
	os.MkdirAll(reportsDir, 0755)

	safeRepoName := strings.ReplaceAll(repo.Name, "/", "-")
	reportPath := filepath.Join(reportsDir, fmt.Sprintf("report-%d-report-%s.md", report.ID, safeRepoName))
	jsonPath := filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-%s.json", report.ID, safeRepoName))

	// Update DB to have these paths
	models.DB.Model(&report).Updates(map[string]interface{}{
		"report_path": reportPath,
	})

	// Pre-create the summary JSON containing a failed chunk with 4 attempts and 0 retries
	initialReport := TaskSummaryReport{
		TaskID:   report.ID,
		RepoName: repo.Name,
		TaskType: taskType.Name,
		Analysis: AnalysisSummary{
			TotalChunks:  1,
			FailedChunks: 1,
			Chunks: []ChunkDetails{
				{
					ChunkName: "root",
					Files:     []string{"file1.go"},
					Status:    "failed",
					Attempts:  4,
					Retries:   0,
				},
			},
		},
	}
	reportData, _ := json.MarshalIndent(initialReport, "", "  ")
	os.WriteFile(jsonPath, reportData, 0644)

	// Also make sure we have a TaskExecutionLog
	execLog := models.TaskExecutionLog{
		TaskReportID: &report.ID,
		Status:       "failed",
	}
	if err := models.DB.Create(&execLog).Error; err != nil {
		t.Fatalf("failed to create execution log: %v", err)
	}
	defer models.DB.Delete(&models.TaskExecutionLog{}, execLog.ID)

	// 5. Run ResumeFailedChunks
	err = ResumeFailedChunks(report.ID)
	if err != nil {
		t.Fatalf("ResumeFailedChunks failed: %v", err)
	}

	// 6. Verify that summary.json was updated with accumulated values
	updatedReportBytes, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read updated report.json: %v", err)
	}

	var updatedReport TaskSummaryReport
	if err := json.Unmarshal(updatedReportBytes, &updatedReport); err != nil {
		t.Fatalf("failed to unmarshal updated report.json: %v", err)
	}

	if len(updatedReport.Analysis.Chunks) != 1 {
		t.Fatalf("expected 1 chunk detail, got %d", len(updatedReport.Analysis.Chunks))
	}

	chunk := updatedReport.Analysis.Chunks[0]
	if chunk.Status != "success" {
		t.Errorf("expected chunk status to be success, got: %s", chunk.Status)
	}

	// 4 previous attempts + 1 current successful attempt = 5
	if chunk.Attempts != 5 {
		t.Errorf("expected cumulative attempts to be 5, got %d", chunk.Attempts)
	}

	// 0 previous retries + 1 current recovery run = 1
	if chunk.Retries != 1 {
		t.Errorf("expected cumulative retries to be 1, got %d", chunk.Retries)
	}
}

type MockSynthesisAIInvoker struct {
	AnalysisCount  int
	SynthesisCount int
	FailSynthesisN int
	WriteEmpty     bool
}

func (m *MockSynthesisAIInvoker) Name() string { return "mock_synthesis_ai" }
func (m *MockSynthesisAIInvoker) Invoke(req AIRequest) error {
	if strings.Contains(req.OutputPath, ".json") {
		m.AnalysisCount++
		os.WriteFile(req.OutputPath, []byte(`{
			"findings": [
				{
					"severity": "建议",
					"category": "test",
					"file_path": "file1.go",
					"line_number": "1",
					"title": "test title",
					"detail": "test detail",
					"suggestion": "test suggestion"
				}
			],
			"summary": "mock summary"
		}`), 0644)
		return nil
	}

	m.SynthesisCount++
	if m.SynthesisCount <= m.FailSynthesisN {
		return fmt.Errorf("simulated synthesis error %d", m.SynthesisCount)
	}

	if m.WriteEmpty {
		os.WriteFile(req.OutputPath, []byte(""), 0644)
		return nil
	}

	os.WriteFile(req.OutputPath, []byte("# Synthesis Success Report"), 0644)
	return nil
}

func TestSynthesisFailureAndRetries(t *testing.T) {
	// 1. Initialize DB
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{}, &models.AnalysisFinding{})

	// 2. Setup mock repository and git structure
	tempDir, err := os.MkdirTemp("", "test-synthesis-repo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repoCodesPath := filepath.Join(tempDir, "git-source")
	os.MkdirAll(repoCodesPath, 0755)

	exec.Command("git", "-C", repoCodesPath, "init").Run()
	os.WriteFile(filepath.Join(repoCodesPath, "file1.go"), []byte("package main"), 0644)
	exec.Command("git", "-C", repoCodesPath, "config", "user.name", "test").Run()
	exec.Command("git", "-C", repoCodesPath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoCodesPath, "add", ".").Run()
	exec.Command("git", "-C", repoCodesPath, "commit", "-m", "init").Run()

	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "test-dept-synthesis-" + ts}
	if err := models.DB.Create(&dept).Error; err != nil {
		t.Fatalf("failed to create department: %v", err)
	}
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-user-synthesis-" + ts, Email: "test-synthesis-" + ts + "@test.com"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-synthesis-repo-" + ts,
		URL:          repoCodesPath,
	}
	if err := models.DB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	// Make sure storage config points to our tempDir
	models.AppConfig.Storage.Root = tempDir

	t.Run("synthesis eventually succeeds", func(t *testing.T) {
		taskType := models.TaskType{
			Name:        "code_review_eventual_success_" + ts,
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_eventual_success_backend",
		}
		if err := models.DB.Create(&taskType).Error; err != nil {
			t.Fatalf("failed to create task type: %v", err)
		}
		defer models.DB.Delete(&models.TaskType{}, taskType.ID)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		if err := models.DB.Create(&report).Error; err != nil {
			t.Fatalf("failed to create report: %v", err)
		}
		defer models.DB.Delete(&models.TaskReport{}, report.ID)

		invoker := &MockSynthesisAIInvoker{FailSynthesisN: 2}
		RegisterAIInvoker(taskType.AIBackend, invoker)

		err := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{})
		if err != nil {
			t.Fatalf("expected RunTaskSync to succeed eventually, got error: %v", err)
		}

		var updatedReport models.TaskReport
		models.DB.First(&updatedReport, report.ID)
		if updatedReport.Status != "success" {
			t.Errorf("expected report status to be success, got: %s", updatedReport.Status)
		}

		if invoker.SynthesisCount != 3 {
			t.Errorf("expected synthesis to be attempted 3 times, got %d", invoker.SynthesisCount)
		}

		content, err := os.ReadFile(updatedReport.GetAbsReportPath())
		if err != nil {
			t.Fatalf("failed to read report: %v", err)
		}
		if !strings.Contains(string(content), "# Synthesis Success Report") {
			t.Errorf("expected report to contain success content, got: %s", string(content))
		}
	})

	t.Run("synthesis all attempts fail", func(t *testing.T) {
		taskType := models.TaskType{
			Name:        "code_review_always_fail_" + ts,
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_always_fail_backend",
		}
		if err := models.DB.Create(&taskType).Error; err != nil {
			t.Fatalf("failed to create task type: %v", err)
		}
		defer models.DB.Delete(&models.TaskType{}, taskType.ID)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		if err := models.DB.Create(&report).Error; err != nil {
			t.Fatalf("failed to create report: %v", err)
		}
		defer models.DB.Delete(&models.TaskReport{}, report.ID)

		invoker := &MockSynthesisAIInvoker{FailSynthesisN: 5}
		RegisterAIInvoker(taskType.AIBackend, invoker)

		err := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{})
		if err == nil {
			t.Fatal("expected RunTaskSync to fail, got nil")
		}

		var updatedReport models.TaskReport
		models.DB.First(&updatedReport, report.ID)
		if updatedReport.Status != "failed" {
			t.Errorf("expected report status to be failed, got: %s", updatedReport.Status)
		}

		if invoker.SynthesisCount != 4 {
			t.Errorf("expected synthesis to be attempted 4 times (1 initial + 3 retries), got %d", invoker.SynthesisCount)
		}

		// Verify output.txt exists and contains the failure
		outputPath := updatedReport.GetAbsReportPath() + ".output.txt"
		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("failed to read output.txt: %v", err)
		}
		if !strings.Contains(string(content), "[Code-Shield Error] AI execution failed: synthesis failed after 3 retries") {
			t.Errorf("expected output.txt to contain synthesis error, got: %s", string(content))
		}
	})

	t.Run("synthesis generates empty file", func(t *testing.T) {
		taskType := models.TaskType{
			Name:        "code_review_empty_file_" + ts,
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_empty_file_backend",
		}
		if err := models.DB.Create(&taskType).Error; err != nil {
			t.Fatalf("failed to create task type: %v", err)
		}
		defer models.DB.Delete(&models.TaskType{}, taskType.ID)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		if err := models.DB.Create(&report).Error; err != nil {
			t.Fatalf("failed to create report: %v", err)
		}
		defer models.DB.Delete(&models.TaskReport{}, report.ID)

		invoker := &MockSynthesisAIInvoker{WriteEmpty: true}
		RegisterAIInvoker(taskType.AIBackend, invoker)

		err := RunTaskSync(report.ID, repo.URL, taskType.ID, false, models.RunParams{})
		if err == nil {
			t.Fatal("expected RunTaskSync to fail, got nil")
		}

		var updatedReport models.TaskReport
		models.DB.First(&updatedReport, report.ID)
		if updatedReport.Status != "failed" {
			t.Errorf("expected report status to be failed, got: %s", updatedReport.Status)
		}

		if invoker.SynthesisCount != 4 {
			t.Errorf("expected synthesis to be attempted 4 times, got %d", invoker.SynthesisCount)
		}
	})
}

func TestIsSourceFileWithTaskExtensions(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		taskExtensions map[string]bool
		expected       bool
	}{
		// ── nil taskExtensions → 回退到全局白名单 ──
		{name: "nil extensions, .go file", file: "main.go", taskExtensions: nil, expected: true},
		{name: "nil extensions, .py file", file: "app.py", taskExtensions: nil, expected: true},
		{name: "nil extensions, .txt file", file: "readme.txt", taskExtensions: nil, expected: false},
		{name: "nil extensions, Makefile", file: "Makefile", taskExtensions: nil, expected: true},
		{name: "nil extensions, Dockerfile", file: "Dockerfile", taskExtensions: nil, expected: true},

		// ── Python-only filter ──
		{name: "py filter, .py file", file: "app.py", taskExtensions: map[string]bool{".py": true}, expected: true},
		{name: "py filter, .go file rejected", file: "main.go", taskExtensions: map[string]bool{".py": true}, expected: false},
		{name: "py filter, .java file rejected", file: "App.java", taskExtensions: map[string]bool{".py": true}, expected: false},
		{name: "py filter, .c file rejected", file: "core.c", taskExtensions: map[string]bool{".py": true}, expected: false},
		{name: "py filter, nested .py file", file: "services/billing.py", taskExtensions: map[string]bool{".py": true}, expected: true},
		{name: "py filter, extensionless Makefile rejected", file: "Makefile", taskExtensions: map[string]bool{".py": true}, expected: false},

		// ── C/C++ filter ──
		{name: "cpp filter, .c file", file: "core.c", taskExtensions: map[string]bool{".c": true, ".cpp": true, ".h": true, ".hpp": true}, expected: true},
		{name: "cpp filter, .cpp file", file: "engine.cpp", taskExtensions: map[string]bool{".c": true, ".cpp": true, ".h": true, ".hpp": true}, expected: true},
		{name: "cpp filter, .h file", file: "include/api.h", taskExtensions: map[string]bool{".c": true, ".cpp": true, ".h": true, ".hpp": true}, expected: true},
		{name: "cpp filter, .py rejected", file: "script.py", taskExtensions: map[string]bool{".c": true, ".cpp": true, ".h": true, ".hpp": true}, expected: false},
		{name: "cpp filter, .go rejected", file: "server.go", taskExtensions: map[string]bool{".c": true, ".cpp": true, ".h": true, ".hpp": true}, expected: false},

		// ── 隐藏目录和 vendor 目录排除（无论 taskExtensions 如何都应排除）──
		{name: "hidden dir excluded with filter", file: ".github/workflows/ci.py", taskExtensions: map[string]bool{".py": true}, expected: false},
		{name: "vendor dir excluded with filter", file: "vendor/lib.py", taskExtensions: map[string]bool{".py": true}, expected: false},
		{name: "node_modules excluded with filter", file: "node_modules/pkg/index.js", taskExtensions: map[string]bool{".js": true}, expected: false},
		{name: "__pycache__ excluded with filter", file: "__pycache__/mod.py", taskExtensions: map[string]bool{".py": true}, expected: false},

		// ── 大小写不敏感 ──
		{name: "case insensitive .PY", file: "app.PY", taskExtensions: map[string]bool{".py": true}, expected: true},
		{name: "case insensitive .Cpp", file: "main.Cpp", taskExtensions: map[string]bool{".cpp": true}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSourceFile(tt.file, tt.taskExtensions)
			if got != tt.expected {
				t.Errorf("isSourceFile(%q, %v) = %v, want %v", tt.file, tt.taskExtensions, got, tt.expected)
			}
		})
	}
}

func TestScanAndChunkWithFileExtensions(t *testing.T) {
	// Setup a temp git repo with mixed language files
	tempDir, err := os.MkdirTemp("", "test-scan-chunk-ext-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	exec.Command("git", "-C", tempDir, "init").Run()
	exec.Command("git", "-C", tempDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()

	// Create mixed files
	files := map[string]string{
		"app.py":        "print('hello')",
		"utils/calc.py": "def add(a, b): return a + b",
		"main.go":       "package main",
		"server.go":     "package main",
		"core.c":        "int main() {}",
		"include/api.h": "#pragma once",
		"lib.java":      "public class Lib {}",
		"README.md":     "# Readme",
		"test_app.py":   "import pytest",
	}
	for name, content := range files {
		fullPath := filepath.Join(tempDir, name)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}
	exec.Command("git", "-C", tempDir, "add", ".").Run()
	exec.Command("git", "-C", tempDir, "commit", "-m", "init").Run()

	t.Run("no file_extensions filter (all source files)", func(t *testing.T) {
		cfg := ChunkConfig{MaxFiles: 100, Depth: 1}
		chunks, err := scanAndChunk(tempDir, cfg, "all")
		if err != nil {
			t.Fatalf("scanAndChunk failed: %v", err)
		}

		totalFiles := 0
		for _, files := range chunks {
			totalFiles += len(files)
		}
		// Should include .py(3), .go(2), .c(1), .h(1), .java(1) = 8 source files, exclude .md
		if totalFiles != 8 {
			t.Errorf("expected 8 source files with no filter, got %d", totalFiles)
			for name, files := range chunks {
				t.Logf("chunk %q: %v", name, files)
			}
		}
	})

	t.Run("Python-only filter", func(t *testing.T) {
		cfg := ChunkConfig{MaxFiles: 100, Depth: 1, FileExtensions: []string{".py"}}
		chunks, err := scanAndChunk(tempDir, cfg, "all")
		if err != nil {
			t.Fatalf("scanAndChunk failed: %v", err)
		}

		totalFiles := 0
		for _, files := range chunks {
			totalFiles += len(files)
			for _, f := range files {
				if !strings.HasSuffix(f, ".py") {
					t.Errorf("unexpected non-.py file in chunk: %s", f)
				}
			}
		}
		// Should include app.py, utils/calc.py, test_app.py
		if totalFiles != 3 {
			t.Errorf("expected 3 .py files, got %d", totalFiles)
		}
	})

	t.Run("Python-only with business scope", func(t *testing.T) {
		cfg := ChunkConfig{MaxFiles: 100, Depth: 1, FileExtensions: []string{".py"}}
		chunks, err := scanAndChunk(tempDir, cfg, "business")
		if err != nil {
			t.Fatalf("scanAndChunk failed: %v", err)
		}

		totalFiles := 0
		for _, files := range chunks {
			totalFiles += len(files)
		}
		// Should include app.py, utils/calc.py (test_app.py excluded by business scope)
		if totalFiles != 2 {
			t.Errorf("expected 2 business .py files, got %d", totalFiles)
		}
	})

	t.Run("C/C++ filter", func(t *testing.T) {
		cfg := ChunkConfig{MaxFiles: 100, Depth: 1, FileExtensions: []string{".c", ".h"}}
		chunks, err := scanAndChunk(tempDir, cfg, "all")
		if err != nil {
			t.Fatalf("scanAndChunk failed: %v", err)
		}

		totalFiles := 0
		for _, files := range chunks {
			totalFiles += len(files)
			for _, f := range files {
				ext := strings.ToLower(filepath.Ext(f))
				if ext != ".c" && ext != ".h" {
					t.Errorf("unexpected file in chunk: %s (ext=%s)", f, ext)
				}
			}
		}
		// Should include core.c, include/api.h
		if totalFiles != 2 {
			t.Errorf("expected 2 C/H files, got %d", totalFiles)
		}
	})

	t.Run("extension without dot prefix", func(t *testing.T) {
		cfg := ChunkConfig{MaxFiles: 100, Depth: 1, FileExtensions: []string{"py"}}
		chunks, err := scanAndChunk(tempDir, cfg, "all")
		if err != nil {
			t.Fatalf("scanAndChunk failed: %v", err)
		}

		totalFiles := 0
		for _, files := range chunks {
			totalFiles += len(files)
		}
		// "py" should be auto-normalized to ".py"
		if totalFiles != 3 {
			t.Errorf("expected 3 .py files with 'py' (no dot), got %d", totalFiles)
		}
	})
}

func TestGetFilteredFilesWithKeywordsAndExcludes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-filter-keyword-exclude-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	exec.Command("git", "-C", tempDir, "init").Run()
	exec.Command("git", "-C", tempDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()

	files := map[string]string{
		"main.c":                  "// use cJSON_Parse to decode json\ncJSON *root = cJSON_Parse(data);",
		"utils.c":                 "int add(int a, int b) { return a + b; }",
		"thirdparts/json_impl.c":  "// cJSON_Parse is here too, but in thirdparts\ncJSON *node = cJSON_Parse(data);",
		"third_party/other_lib.c": "// cJSON_CreateObject is also third party\ncJSON *obj = cJSON_CreateObject();",
		"docs/readme.txt":         "this is just documentation about cJSON",
	}

	for name, content := range files {
		fullPath := filepath.Join(tempDir, name)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	exec.Command("git", "-C", tempDir, "add", ".").Run()
	exec.Command("git", "-C", tempDir, "commit", "-m", "init").Run()

	cfg := ChunkConfig{
		MaxFiles:        100,
		Depth:           1,
		FileExtensions:  []string{".c"},
		ContentKeywords: []string{"cJSON_"},
		ExcludePaths:    []string{"thirdparts"},
	}

	t.Run("filter main.c only", func(t *testing.T) {
		// targetScope is "all"
		filtered, err := getFilteredFiles(tempDir, cfg, "all")
		if err != nil {
			t.Fatalf("getFilteredFiles failed: %v", err)
		}

		// 预期只有 main.c 被包含。
		// utils.c: 不包含 cJSON_ (排除)
		// thirdparts/json_impl.c: 在 ExcludePaths 里 (排除)
		// third_party/other_lib.c: 在全局 isSourceFile 过滤 skip 列表包含 "third_party/" (排除)
		// docs/readme.txt: 后缀不是 .c (排除)
		if len(filtered) != 1 {
			t.Fatalf("expected exactly 1 file, got %d: %v", len(filtered), filtered)
		}
		if filtered[0] != "main.c" {
			t.Errorf("expected filtered file to be 'main.c', got '%s'", filtered[0])
		}
	})
}

type MockAIInvokerForMatch struct{}

func (m *MockAIInvokerForMatch) Name() string { return "mock_match_backend" }
func (m *MockAIInvokerForMatch) Invoke(req AIRequest) error {
	isSame := "false"
	if strings.Contains(req.PromptMsg, "cJSON Memory Leak in parse_config") &&
		strings.Contains(req.PromptMsg, "cJSON Memory Leak in parse_config (Updated)") {
		isSame = "true"
	}

	jsonResult := fmt.Sprintf(`{"is_same": %s}`, isSame)
	_ = os.MkdirAll(filepath.Dir(req.OutputPath), 0755)
	return os.WriteFile(req.OutputPath, []byte(jsonResult), 0644)
}

func TestCampaignHooks(t *testing.T) {
	// 1. 初始化 PostgreSQL 数据库
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()
	var err error

	mustMigrate(t, models.DB,
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.CampaignFinding{},
	)

	// 注册并配置 Mock AI Backend
	mockInvoker := &MockAIInvokerForMatch{}
	RegisterAIInvoker("mock_match_backend", mockInvoker)
	oldBackend := models.AppConfig.AI.Backend
	models.AppConfig.AI.Backend = "mock_match_backend"
	defer func() {
		models.AppConfig.AI.Backend = oldBackend
	}()

	// 2. 初始化基本实体
	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	dept := models.Department{Name: "test-dept-" + uniqueSuffix}
	models.DB.Create(&dept)
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "userx-" + uniqueSuffix, Name: "UserX", Email: "userx_" + uniqueSuffix + "@test.com", Password: "pwd"}
	models.DB.Create(&user)
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-repo-" + uniqueSuffix,
		URL:          "http://xxx.git",
	}
	models.DB.Create(&repo)
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:           "cjson_scan_" + uniqueSuffix,
		DisplayName:    "cJSON内存泄露",
		IsCampaign:     true,
		GovernanceMode: models.GovernanceModeDefectTracking,
	}
	models.DB.Create(&taskType)
	defer models.DB.Delete(&models.TaskType{}, taskType.ID)

	defer func() {
		models.DB.Where("task_type_id = ? AND repo_id = ?", taskType.ID, repo.ID).Delete(&models.CampaignFinding{})
		models.DB.Where("repo_id = ?", repo.ID).Delete(&models.TaskReport{})
	}()

	report1 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: "success"}
	models.DB.Create(&report1)

	ctx1 := &taskContext{
		repo:     repo,
		report:   report1,
		taskType: taskType,
	}

	// 3. 第一次扫描：发现 2 个缺陷
	finding1 := models.AnalysisFinding{
		FilePath:    "src/main.c",
		LineNumber:  "55",
		Title:       "cJSON Memory Leak in parse_config",
		Detail:      "cJSON object allocated but not deleted on error path.",
		CodeSnippet: "cJSON *json = cJSON_Parse(data);\nif (!json) return;",
		Severity:    "严重",
		Category:    "memory_leak",
		Suggestion:  "Call cJSON_Delete(json) before returning.",
	}
	finding2 := models.AnalysisFinding{
		FilePath:    "src/utils.c",
		LineNumber:  "120",
		Title:       "cJSON leak in process",
		Detail:      "Memory leak at cJSON object creation.",
		CodeSnippet: "cJSON *item = cJSON_CreateObject();",
		Severity:    "一般",
		Category:    "memory_leak",
		Suggestion:  "Call cJSON_Delete(item).",
	}

	err = handleGenericCampaignHook(ctx1, []models.AnalysisFinding{finding1, finding2})
	if err != nil {
		t.Fatalf("handleGenericCampaignHook failed on scan 1: %v", err)
	}

	// 验证数据正确入库
	var dbFindings []models.CampaignFinding
	models.DB.Where("task_type_id = ? AND repo_id = ?", taskType.ID, repo.ID).Find(&dbFindings)
	if len(dbFindings) != 2 {
		t.Fatalf("expected 2 findings in DB, got %d", len(dbFindings))
	}

	// 4. 开发人员修改 Finding 属性以测试“人工数据保护”
	var f1 models.CampaignFinding
	models.DB.Where("task_type_id = ? AND repo_id = ? AND file_path = ? AND line_number = ?", taskType.ID, repo.ID, "src/main.c", "55").First(&f1)
	assigneeID := user.ID
	f1.AssigneeID = &assigneeID
	f1.Status = "invalid"
	models.DB.Save(&f1)

	// 5. 第二次扫描：
	report2 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: "success"}
	models.DB.Create(&report2)
	ctx2 := &taskContext{
		repo:     repo,
		report:   report2,
		taskType: taskType,
	}

	finding1Updated := models.AnalysisFinding{
		FilePath:    "src/main.c",
		LineNumber:  "55-63",
		Title:       "cJSON Memory Leak in parse_config (Updated)",
		Detail:      "cJSON object leak",
		CodeSnippet: "cJSON *json = cJSON_Parse(data);\nif (!json) return;",
		Severity:    "严重",
		Category:    "memory_leak",
		Suggestion:  "Fix it",
	}
	finding2Resolved := models.AnalysisFinding{
		FilePath:    "src/utils.c",
		LineNumber:  "120",
		Title:       "cJSON leak in process",
		Detail:      "Memory leak at cJSON object creation.",
		CodeSnippet: "cJSON *item = cJSON_CreateObject();",
		Severity:    "合格",
		Category:    "memory_leak",
		Suggestion:  "Call cJSON_Delete(item).",
	}
	finding3 := models.AnalysisFinding{
		FilePath:    "src/main.c",
		LineNumber:  "210",
		Title:       "cJSON leak in main",
		Detail:      "Leaked at main exit.",
		CodeSnippet: "cJSON *root = cJSON_CreateArray();",
		Severity:    "严重",
		Category:    "memory_leak",
		Suggestion:  "Delete it.",
	}

	err = handleGenericCampaignHook(ctx2, []models.AnalysisFinding{finding1Updated, finding2Resolved, finding3})
	if err != nil {
		t.Fatalf("handleGenericCampaignHook failed on scan 2: %v", err)
	}

	// 6. 验证合并和覆盖保护结果
	var f1After models.CampaignFinding
	models.DB.Where("task_type_id = ? AND repo_id = ? AND file_path = ? AND line_number = ?", taskType.ID, repo.ID, "src/main.c", "55-63").First(&f1After)
	if f1After.ID == 0 {
		t.Fatalf("Finding 1 (shifted line number) was not matched/updated")
	}
	if f1After.Status != "invalid" {
		t.Errorf("expected User Status 'invalid' to be preserved, got '%s'", f1After.Status)
	}
	if f1After.AssigneeID == nil || *f1After.AssigneeID != user.ID {
		t.Errorf("expected AssigneeID = %d to be preserved", user.ID)
	}

	// 验证 Finding 1 的 StatusLog 中 confirm_count 递增至 2 且包含 last_confirmed_at
	var f1Logs []map[string]interface{}
	if err := json.Unmarshal(f1After.StatusLog, &f1Logs); err != nil || len(f1Logs) == 0 {
		t.Fatalf("failed to unmarshal f1After.StatusLog: %v", err)
	}
	if f1Logs[0]["confirm_count"] == nil || int(f1Logs[0]["confirm_count"].(float64)) != 2 {
		t.Errorf("expected confirm_count = 2 for finding 1 on scan 2, got %v", f1Logs[0]["confirm_count"])
	}
	if f1Logs[0]["last_confirmed_at"] == nil || f1Logs[0]["last_confirmed_at"].(string) == "" {
		t.Errorf("expected last_confirmed_at to be populated for finding 1 on scan 2")
	}

	// 验证 Finding 3 入库
	var f3After models.CampaignFinding
	models.DB.Where("task_type_id = ? AND repo_id = ? AND file_path = ? AND line_number = ?", taskType.ID, repo.ID, "src/main.c", "210").First(&f3After)
	if f3After.ID == 0 {
		t.Errorf("Finding 3 was not created")
	}
	var f3Logs []map[string]interface{}
	if err := json.Unmarshal(f3After.StatusLog, &f3Logs); err != nil || len(f3Logs) == 0 {
		t.Fatalf("failed to unmarshal f3After.StatusLog: %v", err)
	}
	if f3Logs[0]["confirm_count"] == nil || int(f3Logs[0]["confirm_count"].(float64)) != 1 {
		t.Errorf("expected initial confirm_count = 1 for finding 3, got %v", f3Logs[0]["confirm_count"])
	}

	// 7. 测试“逻辑消亡（不物理删除）”：
	findingObsolete := models.CampaignFinding{
		TaskTypeID:   taskType.ID,
		RepoID:       repo.ID,
		TaskReportID: report2.ID,
		FilePath:     "src/obsolete.c",
		LineNumber:   "90",
		Title:        "Unresolved leak",
		Status:       "open",
	}
	models.DB.Create(&findingObsolete)

	report3 := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: "success"}
	models.DB.Create(&report3)
	ctx3 := &taskContext{
		repo:     repo,
		report:   report3,
		taskType: taskType,
	}

	err = handleGenericCampaignHook(ctx3, []models.AnalysisFinding{finding3})
	if err != nil {
		t.Fatalf("handleGenericCampaignHook failed on scan 3: %v", err)
	}

	var fObsAfter models.CampaignFinding
	err = models.DB.Where("file_path = ?", "src/obsolete.c").First(&fObsAfter).Error
	if err != nil {
		t.Fatalf("obsolete finding was physically deleted: %v", err)
	}
	if fObsAfter.Status != "resolved" {
		t.Errorf("expected obsolete finding to be logically resolved, got '%s'", fObsAfter.Status)
	}
}

func TestPrepareAndSync_BranchSwitching(t *testing.T) {
	// 1. 初始化临时目录与远程 Git 仓库
	tempBase, err := os.MkdirTemp("", "test-prepare-sync-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempBase)

	remoteRepoDir := filepath.Join(tempBase, "remote-repo")
	if err := os.MkdirAll(remoteRepoDir, 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}

	exec.Command("git", "-C", remoteRepoDir, "init").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.email", "test@test.com").Run()

	// 在 master 分支提交文件
	os.WriteFile(filepath.Join(remoteRepoDir, "master.txt"), []byte("master content"), 0644)
	exec.Command("git", "-C", remoteRepoDir, "add", ".").Run()
	exec.Command("git", "-C", remoteRepoDir, "commit", "-m", "init master").Run()

	// 创建并切换到 feature-branch 分支，提交 feature 文件
	exec.Command("git", "-C", remoteRepoDir, "checkout", "-b", "feature-branch").Run()
	os.WriteFile(filepath.Join(remoteRepoDir, "feature.txt"), []byte("feature content"), 0644)
	exec.Command("git", "-C", remoteRepoDir, "add", ".").Run()
	exec.Command("git", "-C", remoteRepoDir, "commit", "-m", "feature commit").Run()

	// 切换回 master 分支
	exec.Command("git", "-C", remoteRepoDir, "checkout", "master").Run()

	// 2. 初始化测试数据库和配置
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{})

	models.AppConfig.Storage.Root = filepath.Join(tempBase, "storage")
	_ = os.MkdirAll(models.AppConfig.GetDataDir(), 0755)

	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "test-dept-" + ts}
	if err := testDB.Create(&dept).Error; err != nil {
		t.Fatalf("failed to create department: %v", err)
	}
	defer testDB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "test-user-" + ts, Email: "test-user-" + ts + "@test.com"}
	if err := testDB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	defer testDB.Delete(&models.User{}, user.ID)

	taskType := models.TaskType{
		Name:        "test-type-" + ts,
		DisplayName: "测试类型",
	}
	if err := testDB.Create(&taskType).Error; err != nil {
		t.Fatalf("failed to create task type: %v", err)
	}
	defer testDB.Delete(&models.TaskType{}, taskType.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-branch-repo-" + ts,
		URL:          "file://" + remoteRepoDir,
		Branch:       "feature-branch",
	}
	if err := testDB.Create(&repo).Error; err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer testDB.Delete(&models.Repository{}, repo.ID)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "pending",
	}
	if err := testDB.Create(&report).Error; err != nil {
		t.Fatalf("failed to create report: %v", err)
	}
	defer testDB.Delete(&models.TaskReport{}, report.ID)

	taskCtx := &taskContext{
		ctx:      context.Background(),
		repo:     repo,
		report:   report,
		taskType: taskType,
	}

	// 3. 首次同步：应当直接克隆并切换到 feature-branch
	if err := taskCtx.prepareAndSync(repo.URL); err != nil {
		t.Fatalf("prepareAndSync failed on feature-branch: %v", err)
	}

	// 验证 feature.txt 存在
	if _, err := os.Stat(filepath.Join(taskCtx.codesPath, "feature.txt")); os.IsNotExist(err) {
		t.Errorf("expected feature.txt to exist in checked out feature-branch")
	}

	// 4. 第二次同步：仓库切换回 master 分支并拉取
	taskCtx.repo.Branch = "master"
	if err := taskCtx.prepareAndSync(repo.URL); err != nil {
		t.Fatalf("prepareAndSync failed on switching to master: %v", err)
	}

	// 验证 master 分支状态：master.txt 存在，而 feature.txt 不应存在于 master
	if _, err := os.Stat(filepath.Join(taskCtx.codesPath, "master.txt")); os.IsNotExist(err) {
		t.Errorf("expected master.txt to exist in checked out master branch")
	}
	if _, err := os.Stat(filepath.Join(taskCtx.codesPath, "feature.txt")); !os.IsNotExist(err) {
		t.Errorf("expected feature.txt NOT to exist in checked out master branch")
	}

	// 验证数据库状态已记录为 success
	var updatedReport models.TaskReport
	models.DB.First(&updatedReport, report.ID)
	if updatedReport.CloneStatus != "success" {
		t.Errorf("expected clone_status to be 'success', got '%s'", updatedReport.CloneStatus)
	}
}

func TestPrepareAndSync_StaleLockAutoHealing(t *testing.T) {
	tempBase, err := os.MkdirTemp("", "test-stale-lock-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempBase)

	remoteRepoDir := filepath.Join(tempBase, "remote-repo")
	_ = os.MkdirAll(remoteRepoDir, 0755)
	exec.Command("git", "-C", remoteRepoDir, "init").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.email", "test@test.com").Run()
	os.WriteFile(filepath.Join(remoteRepoDir, "file.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", remoteRepoDir, "add", ".").Run()
	exec.Command("git", "-C", remoteRepoDir, "commit", "-m", "init").Run()

	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{})

	models.AppConfig.Storage.Root = filepath.Join(tempBase, "storage")
	_ = os.MkdirAll(models.AppConfig.GetDataDir(), 0755)

	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "dept-" + ts}
	testDB.Create(&dept)
	user := models.User{Username: "user-" + ts, Email: "user-" + ts + "@test.com"}
	testDB.Create(&user)
	taskType := models.TaskType{Name: "type-" + ts, DisplayName: "测试类型"}
	testDB.Create(&taskType)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "stale-lock-repo-" + ts,
		URL:          "file://" + remoteRepoDir,
		Branch:       "master",
	}
	testDB.Create(&repo)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "pending",
	}
	testDB.Create(&report)

	taskCtx := &taskContext{
		ctx:      context.Background(),
		repo:     repo,
		report:   report,
		taskType: taskType,
	}

	// 1. 首次同步
	if err := taskCtx.prepareAndSync(repo.URL); err != nil {
		t.Fatalf("first prepareAndSync failed: %v", err)
	}

	// 2. 人为制造异常中断残留的 .git/index.lock
	lockPath := filepath.Join(taskCtx.codesPath, ".git", "index.lock")
	if err := os.WriteFile(lockPath, []byte("stale-lock-pid-99999"), 0644); err != nil {
		t.Fatalf("failed to create fake stale index.lock: %v", err)
	}

	// 3. 再次触发同步：应当自动自愈清理 index.lock，且不发生错误
	if err := taskCtx.prepareAndSync(repo.URL); err != nil {
		t.Fatalf("prepareAndSync failed to auto-heal stale git lock: %v", err)
	}

	// 验证锁文件已被自愈清除
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("expected index.lock to be cleaned up, but it still exists")
	}
}

func TestPrepareAndSync_ConcurrentSafe(t *testing.T) {
	tempBase, err := os.MkdirTemp("", "test-concurrent-sync-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempBase)

	remoteRepoDir := filepath.Join(tempBase, "remote-repo")
	_ = os.MkdirAll(remoteRepoDir, 0755)
	exec.Command("git", "-C", remoteRepoDir, "init").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.name", "test").Run()
	exec.Command("git", "-C", remoteRepoDir, "config", "user.email", "test@test.com").Run()
	os.WriteFile(filepath.Join(remoteRepoDir, "file.txt"), []byte("content"), 0644)
	exec.Command("git", "-C", remoteRepoDir, "add", ".").Run()
	exec.Command("git", "-C", remoteRepoDir, "commit", "-m", "init").Run()

	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	models.DB = testDB
	mustMigrate(t, models.DB, &models.Department{}, &models.User{}, &models.TaskReport{}, &models.Repository{}, &models.TaskType{})

	models.AppConfig.Storage.Root = filepath.Join(tempBase, "storage")
	_ = os.MkdirAll(models.AppConfig.GetDataDir(), 0755)

	ts := time.Now().Format("150405.000000")
	dept := models.Department{Name: "dept-" + ts}
	testDB.Create(&dept)
	user := models.User{Username: "user-" + ts, Email: "user-" + ts + "@test.com"}
	testDB.Create(&user)
	taskType := models.TaskType{Name: "type-" + ts, DisplayName: "测试类型"}
	testDB.Create(&taskType)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "concurrent-repo-" + ts,
		URL:          "file://" + remoteRepoDir,
		Branch:       "master",
	}
	testDB.Create(&repo)

	// 并发 6 个任务同时对同一个物理仓库发起同步
	concurrency := 6
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			report := models.TaskReport{
				RepoID:     repo.ID,
				TaskTypeID: taskType.ID,
				Status:     "pending",
			}
			testDB.Create(&report)

			taskCtx := &taskContext{
				ctx:      context.Background(),
				repo:     repo,
				report:   report,
				taskType: taskType,
			}

			if err := taskCtx.prepareAndSync(repo.URL); err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent prepareAndSync produced error: %v", err)
	}
}

func TestCleanStaleGitLocks(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	refsDir := filepath.Join(gitDir, "refs", "heads")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("failed to create fake git dirs: %v", err)
	}

	indexLock := filepath.Join(gitDir, "index.lock")
	shallowLock := filepath.Join(gitDir, "shallow.lock")
	branchLock := filepath.Join(refsDir, "master.lock")
	regularFile := filepath.Join(gitDir, "config")

	_ = os.WriteFile(indexLock, []byte("lock1"), 0644)
	_ = os.WriteFile(shallowLock, []byte("lock2"), 0644)
	_ = os.WriteFile(branchLock, []byte("lock3"), 0644)
	_ = os.WriteFile(regularFile, []byte("[core]"), 0644)

	// 执行清理
	cleanStaleGitLocks(tempDir)

	if _, err := os.Stat(indexLock); !os.IsNotExist(err) {
		t.Errorf("expected index.lock to be deleted")
	}
	if _, err := os.Stat(shallowLock); !os.IsNotExist(err) {
		t.Errorf("expected shallow.lock to be deleted")
	}
	if _, err := os.Stat(branchLock); !os.IsNotExist(err) {
		t.Errorf("expected master.lock to be deleted")
	}
	if _, err := os.Stat(regularFile); os.IsNotExist(err) {
		t.Errorf("expected regular config file to be preserved")
	}
}

func TestGetRepoSyncLock(t *testing.T) {
	lock1 := getRepoSyncLock("/path/to/repoA")
	lock2 := getRepoSyncLock("/path/to/repoA")
	lock3 := getRepoSyncLock("/path/to/repoB")

	if lock1 != lock2 {
		t.Errorf("expected identical mutex instance for the same repo path")
	}
	if lock1 == lock3 {
		t.Errorf("expected different mutex instances for different repo paths")
	}
}

func TestSanitizeFindingTitle(t *testing.T) {
	// 1. 空标题
	if got := SanitizeFindingTitle("   "); got != "未命名缺陷" {
		t.Errorf("expected '未命名缺陷', got %q", got)
	}

	// 2. 换行清洗
	multiLine := "这是第一行标题\n这是第二行描述\r\n这是第三行"
	gotClean := SanitizeFindingTitle(multiLine)
	if strings.Contains(gotClean, "\n") || strings.Contains(gotClean, "\r") {
		t.Errorf("expected newlines removed, got %q", gotClean)
	}

	// 3. 超长字符截断 (超过 500 字符)
	longTitle := strings.Repeat("长标题测试内容abc123", 50) // ~750 runes
	gotTrunc := SanitizeFindingTitle(longTitle)
	if len([]rune(gotTrunc)) > 500 {
		t.Errorf("expected length <= 500 runes, got %d", len([]rune(gotTrunc)))
	}
	if !strings.HasSuffix(gotTrunc, "...") {
		t.Errorf("expected suffix '...', got %q", gotTrunc)
	}
}

func TestDeriveConciseTitle(t *testing.T) {
	// 1. 空文本回退到分类
	if got := deriveConciseTitle("", "CWE-476"); got != "CWE-476" {
		t.Errorf("expected 'CWE-476', got %q", got)
	}

	// 2. 换行文本截取第一行
	multiline := "m_diskLoop 取自服务初始化结构体存在空指针隐患\n后续第二行详细分析..."
	if got := deriveConciseTitle(multiline, ""); got != "m_diskLoop 取自服务初始化结构体存在空指针隐患" {
		t.Errorf("expected first line, got %q", got)
	}

	// 3. 句号截断首个语义完整句
	longSentence := "m_diskLoop 取自全局上下文指针，存在解引用前未判空风险。在函数内部若传入 NULL 会触发 coredump，导致服务不可用。"
	expected := "m_diskLoop 取自全局上下文指针，存在解引用前未判空风险。"
	if got := deriveConciseTitle(longSentence, ""); got != expected {
		t.Errorf("expected first sentence %q, got %q", expected, got)
	}

	// 4. 超长无断句截断 <= 200 runes
	veryLong := strings.Repeat("无断句超长成因描述文本测试内容", 30)
	got := deriveConciseTitle(veryLong, "")
	if len([]rune(got)) > 200 {
		t.Errorf("expected length <= 200, got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected suffix '...', got %q", got)
	}

	// 5. 真实长文本攻击假设截取 (用户报错用例)
	userLogText := "m_diskLoop 取自单例 CPaDiskExtractionZoneGainControl::GetInstance()（函数内 static 局部指针，每次 new 一次 并返回同一个地址）。该对象同时是任务线程宿主：Initialize() 通过 m_taskManager->CreateTask 注册了周期任务 AgentEntry（闭环保底偏离心控制），并且 CPaDeviceManager::InitLoop/UnInitLoop 也通过同样的 GetInstance() 获取并使用该指针。一旦 CPPPAPServiceTImpl 析构（服务卸载/重载/进程 teardown 路径）执行 delete m_diskLoop 释放单例，static sInstance 仍指向已释放内存（delete 不会清空 static 局部指针）。此后任何一次后续的 GetInstance()（例如 CPaDeviceManager::UnInitLoop、或仍处于活动状态周期任务 AgentEntry 继续运行）都会对悬空指针解引用，产生 use-after-free，访问已释放对象内存，在取到非法地址或 vtable 已回收时触发 SIGSEGV 导致 coredump。"
	userTitle := deriveConciseTitle(userLogText, "CWE-416")
	if len([]rune(userTitle)) > 150 {
		t.Errorf("expected concise title <= 150 runes, got %d (%s)", len([]rune(userTitle)), userTitle)
	}
	expectedUserTitle := "m_diskLoop 取自单例 CPaDiskExtractionZoneGainControl::GetInstance()（函数内 static 局部指针，每次 new 一次 并返回同一个地址）。"
	if userTitle != expectedUserTitle {
		t.Errorf("expected %q, got %q", expectedUserTitle, userTitle)
	}
}

func TestHandleGenericCampaignHook_LongTitle(t *testing.T) {
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()

	mustMigrate(t, models.DB,
		&models.Department{},
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.CampaignFinding{},
	)

	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	dept := models.Department{Name: "test-dept-long-" + uniqueSuffix}
	models.DB.Create(&dept)
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "user-long-" + uniqueSuffix, Name: "UserLong", Email: "user_long_" + uniqueSuffix + "@test.com", Password: "pwd"}
	models.DB.Create(&user)
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-repo-long-" + uniqueSuffix,
		URL:          "http://xxx.git",
	}
	models.DB.Create(&repo)
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:           "coredump_scan_" + uniqueSuffix,
		DisplayName:    "Coredump风险",
		IsCampaign:     true,
		GovernanceMode: models.GovernanceModeDefectTracking,
	}
	models.DB.Create(&taskType)
	defer models.DB.Delete(&models.TaskType{}, taskType.ID)

	defer func() {
		models.DB.Where("task_type_id = ? AND repo_id = ?", taskType.ID, repo.ID).Delete(&models.CampaignFinding{})
		models.DB.Where("repo_id = ?", repo.ID).Delete(&models.TaskReport{})
	}()

	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: "success"}
	models.DB.Create(&report)

	ctx := &taskContext{
		repo:     repo,
		report:   report,
		taskType: taskType,
	}

	// 构造一个超过 500 字符的超长标题（模拟真实大模型输出的段落级分析内容）
	originalLongTitle := "m_diskLoop 取自 " + strings.Repeat("服务上下文全局单例对象的属性指针并直接进行访问，未进行显式有效性校验与判空保护，可能在极端并发或初始化未完成场景下产生严重内存崩溃；", 10)
	finding := models.AnalysisFinding{
		FilePath:    "dlPrePulseLaser/pppacp/src/CPPPAPServiceTImpl.cpp",
		LineNumber:  "41-49",
		Title:       originalLongTitle,
		Detail:      "现有代码存在直接解引用风险。",
		CodeSnippet: "m_diskLoop->process();",
		Severity:    "严重",
		Category:    "coredump_risk",
		Suggestion:  "建议在调用前增加 if (m_diskLoop != nullptr) 校验保护。",
	}

	err := handleGenericCampaignHook(ctx, []models.AnalysisFinding{finding})
	if err != nil {
		t.Fatalf("handleGenericCampaignHook failed with long title: %v", err)
	}

	var saved models.CampaignFinding
	err = models.DB.Where("task_type_id = ? AND repo_id = ? AND file_path = ?", taskType.ID, repo.ID, finding.FilePath).First(&saved).Error
	if err != nil {
		t.Fatalf("failed to query saved CampaignFinding: %v", err)
	}

	if len([]rune(saved.Title)) > 500 {
		t.Errorf("expected saved title length <= 500, got %d", len([]rune(saved.Title)))
	}
	if !strings.Contains(saved.Detail, "原始问题描述") && !strings.Contains(saved.Detail, "m_diskLoop") {
		t.Errorf("expected saved detail to preserve long title context, got %s", saved.Detail)
	}
}

func TestHandleGenericCampaignHook_PartialFailureTolerance(t *testing.T) {
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()

	mustMigrate(t, models.DB,
		&models.Department{},
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.CampaignFinding{},
	)

	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	dept := models.Department{Name: "test-dept-tol-" + uniqueSuffix}
	models.DB.Create(&dept)
	defer models.DB.Delete(&models.Department{}, dept.ID)

	user := models.User{Username: "user-tol-" + uniqueSuffix, Name: "UserTol", Email: "user_tol_" + uniqueSuffix + "@test.com", Password: "pwd"}
	models.DB.Create(&user)
	defer models.DB.Delete(&models.User{}, user.ID)

	repo := models.Repository{
		DepartmentID: dept.ID,
		OwnerID:      user.ID,
		Name:         "test-repo-tol-" + uniqueSuffix,
		URL:          "http://xxx.git",
	}
	models.DB.Create(&repo)
	defer models.DB.Delete(&models.Repository{}, repo.ID)

	taskType := models.TaskType{
		Name:           "tolerance_scan_" + uniqueSuffix,
		DisplayName:    "容错测试专项",
		IsCampaign:     true,
		GovernanceMode: models.GovernanceModeDefectTracking,
	}
	models.DB.Create(&taskType)
	defer models.DB.Delete(&models.TaskType{}, taskType.ID)

	defer func() {
		models.DB.Where("task_type_id = ? AND repo_id = ?", taskType.ID, repo.ID).Delete(&models.CampaignFinding{})
		models.DB.Where("repo_id = ?", repo.ID).Delete(&models.TaskReport{})
	}()

	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: "success"}
	models.DB.Create(&report)

	ctx := &taskContext{
		repo:     repo,
		report:   report,
		taskType: taskType,
	}

	findings := []models.AnalysisFinding{
		{
			FilePath:    "src/file1.cpp",
			LineNumber:  "10-15",
			Title:       "正常缺陷1",
			Detail:      "正常缺陷描述1",
			Severity:    "严重",
			Category:    "mem_leak",
			CodeSnippet: "char* p = malloc(10);",
		},
		{
			FilePath:    "src/file2.cpp",
			LineNumber:  "20-25",
			Title:       "正常缺陷2",
			Detail:      "正常缺陷描述2",
			Severity:    "一般",
			Category:    "null_deref",
			CodeSnippet: "int *p = nullptr;",
		},
	}

	err := handleGenericCampaignHook(ctx, findings)
	if err != nil {
		t.Fatalf("handleGenericCampaignHook failed: %v", err)
	}

	var count int64
	models.DB.Model(&models.CampaignFinding{}).Where("task_type_id = ? AND repo_id = ?", taskType.ID, repo.ID).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 findings saved, got %d", count)
	}
}
