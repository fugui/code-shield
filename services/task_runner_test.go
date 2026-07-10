package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-shield/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

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
	// 1. Initialize isolated in-memory DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	models.DB = testDB
	err = models.DB.AutoMigrate(&models.TaskReport{}, &models.Repository{}, &models.TaskType{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

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
	repo := models.Repository{
		ID:   1,
		Name: "test-repo",
		URL:  "https://github.com/test/test-repo",
	}
	models.DB.Create(&repo)

	taskType := models.TaskType{
		Name:        "code_review",
		DisplayName: "代码检视",
		EngineMode:  "chunked",
	}
	models.DB.Create(&taskType)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "running",
	}
	models.DB.Create(&report)

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
	// 1. Initialize isolated in-memory DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	models.DB = testDB
	err = models.DB.AutoMigrate(&models.TaskReport{}, &models.Repository{}, &models.TaskType{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	// 2. Setup mock models in DB
	repo := models.Repository{
		ID:   1,
		Name: "test-repo",
		URL:  "https://invalid-url-for-test.git",
	}
	models.DB.Create(&repo)

	taskType := models.TaskType{
		Name:        "code_review",
		DisplayName: "代码检视",
		EngineMode:  "single",
	}
	models.DB.Create(&taskType)

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "running",
	}
	models.DB.Create(&report)

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
	// 1. Initialize isolated in-memory DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	models.DB = testDB
	err = models.DB.AutoMigrate(&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{}, &models.AnalysisFinding{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

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
	repo := models.Repository{
		ID:   1,
		Name: "test-repo",
		URL:  repoCodesPath,
	}
	models.DB.Create(&repo)

	taskType := models.TaskType{
		Name:        "code_review",
		DisplayName: "代码检视",
		EngineMode:  "chunked",
		AIBackend:   "mock_success_backend",
	}
	models.DB.Create(&taskType)

	// Storage config
	models.AppConfig.Storage.Root = tempDir

	report := models.TaskReport{
		RepoID:     repo.ID,
		TaskTypeID: taskType.ID,
		Status:     "failed",
	}
	models.DB.Create(&report)

	// Mock the JSON report path and content
	reportsDir := filepath.Join(tempDir, "reports", taskType.Name, time.Now().Format("2006-01-02"))
	os.MkdirAll(reportsDir, 0755)

	reportPath := filepath.Join(reportsDir, fmt.Sprintf("report-%d-report-test-repo.md", report.ID))
	jsonPath := filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-test-repo.json", report.ID))

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
	models.DB.Create(&execLog)

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
		os.WriteFile(req.OutputPath, []byte(`{"findings": [], "summary": "mock summary"}`), 0644)
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
	// 1. Initialize isolated in-memory DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	models.DB = testDB
	err = models.DB.AutoMigrate(&models.TaskReport{}, &models.Repository{}, &models.TaskType{}, &models.TaskExecutionLog{}, &models.ScheduleConfig{}, &models.AnalysisFinding{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

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

	repo := models.Repository{
		ID:   1,
		Name: "test-repo",
		URL:  repoCodesPath,
	}
	models.DB.Create(&repo)

	// Make sure storage config points to our tempDir
	models.AppConfig.Storage.Root = tempDir

	t.Run("synthesis eventually succeeds", func(t *testing.T) {
		taskType := models.TaskType{
			Name:        "code_review_eventual_success",
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_eventual_success_backend",
		}
		models.DB.Create(&taskType)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		models.DB.Create(&report)

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
			Name:        "code_review_always_fail",
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_always_fail_backend",
		}
		models.DB.Create(&taskType)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		models.DB.Create(&report)

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
			Name:        "code_review_empty_file",
			DisplayName: "代码检视",
			EngineMode:  "single",
			AIBackend:   "mock_synthesis_empty_file_backend",
		}
		models.DB.Create(&taskType)

		report := models.TaskReport{
			RepoID:     repo.ID,
			TaskTypeID: taskType.ID,
			Status:     "running",
		}
		models.DB.Create(&report)

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
	// 1. 初始化 sqlite 内存数据库
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()

	err = models.DB.AutoMigrate(
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.CjsonFinding{},
		&models.TestCaseFinding{},
	)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	// 注册并配置 Mock AI Backend
	mockInvoker := &MockAIInvokerForMatch{}
	RegisterAIInvoker("mock_match_backend", mockInvoker)
	oldBackend := models.AppConfig.AI.Backend
	models.AppConfig.AI.Backend = "mock_match_backend"
	defer func() {
		models.AppConfig.AI.Backend = oldBackend
	}()

	// 2. 初始化基本实体
	repo := models.Repository{ID: 1, Name: "test-repo", URL: "http://xxx.git"}
	models.DB.Create(&repo)

	taskType := models.TaskType{ID: 1, Name: "cjson_scan", DisplayName: "cJSON内存泄露"}
	models.DB.Create(&taskType)

	user := models.User{ID: 1, Name: "UserX", Email: "userx@test.com", Password: "pwd"}
	models.DB.Create(&user)

	report1 := models.TaskReport{ID: 10, RepoID: 1, TaskTypeID: 1, Status: "success"}
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

	err = handleCampaignHook[models.CjsonFinding](ctx1, []models.AnalysisFinding{finding1, finding2})
	if err != nil {
		t.Fatalf("handleCampaignHook failed on scan 1: %v", err)
	}

	// 验证数据正确入库
	var dbFindings []models.CjsonFinding
	models.DB.Find(&dbFindings)
	if len(dbFindings) != 2 {
		t.Fatalf("expected 2 findings in DB, got %d", len(dbFindings))
	}

	// 4. 开发人员修改 Finding 属性以测试“人工数据保护”
	var f1 models.CjsonFinding
	models.DB.Where("file_path = ? AND line_number = ?", "src/main.c", "55").First(&f1)
	assigneeID := uint(1)
	f1.AssigneeID = &assigneeID
	f1.Severity = "建议"
	f1.Status = "invalid"
	models.DB.Save(&f1)

	// 5. 第二次扫描：
	report2 := models.TaskReport{ID: 20, RepoID: 1, TaskTypeID: 1, Status: "success"}
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

	err = handleCampaignHook[models.CjsonFinding](ctx2, []models.AnalysisFinding{finding1Updated, finding2Resolved, finding3})
	if err != nil {
		t.Fatalf("handleCampaignHook failed on scan 2: %v", err)
	}

	// 6. 验证合并和覆盖保护结果
	var f1After models.CjsonFinding
	models.DB.Where("file_path = ? AND line_number = ?", "src/main.c", "55-63").First(&f1After)
	if f1After.ID == 0 {
		t.Fatalf("Finding 1 (shifted line number) was not matched/updated")
	}
	if f1After.Status != "invalid" {
		t.Errorf("expected User Status 'invalid' to be preserved, got '%s'", f1After.Status)
	}
	if f1After.Severity != "建议" {
		t.Errorf("expected User Severity '建议' to be preserved, got '%s'", f1After.Severity)
	}
	if f1After.AssigneeID == nil || *f1After.AssigneeID != 1 {
		t.Errorf("expected AssigneeID = 1 to be preserved")
	}

	// 验证 Finding 2 状态被自动置为 "closed"
	var f2After models.CjsonFinding
	models.DB.Where("file_path = ? AND line_number = ?", "src/utils.c", "120").First(&f2After)
	if f2After.Status != "closed" {
		t.Errorf("expected Finding 2 to be automatically closed, got '%s'", f2After.Status)
	}

	// 验证 Finding 3 入库
	var f3After models.CjsonFinding
	models.DB.Where("file_path = ? AND line_number = ?", "src/main.c", "210").First(&f3After)
	if f3After.ID == 0 {
		t.Errorf("Finding 3 was not created")
	}

	// 7. 测试“逻辑消亡（不物理删除）”：
	findingObsolete := models.CjsonFinding{
		RepoID:       1,
		TaskReportID: 20,
		FilePath:     "src/obsolete.c",
		LineNumber:   "90",
		Title:        "Unresolved leak",
		Status:       "open",
	}
	models.DB.Create(&findingObsolete)

	report3 := models.TaskReport{ID: 30, RepoID: 1, TaskTypeID: 1, Status: "success"}
	models.DB.Create(&report3)
	ctx3 := &taskContext{
		repo:     repo,
		report:   report3,
		taskType: taskType,
	}

	err = handleCampaignHook[models.CjsonFinding](ctx3, []models.AnalysisFinding{finding3})
	if err != nil {
		t.Fatalf("handleCampaignHook failed on scan 3: %v", err)
	}

	var fObsAfter models.CjsonFinding
	err = models.DB.Where("file_path = ?", "src/obsolete.c").First(&fObsAfter).Error
	if err != nil {
		t.Fatalf("obsolete finding was physically deleted: %v", err)
	}
	if fObsAfter.Status != "resolved" {
		t.Errorf("expected obsolete finding to be logically resolved, got '%s'", fObsAfter.Status)
	}
}
