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

	var execReport ChunkExecutionReport
	if err := json.Unmarshal(reportBytes, &execReport); err != nil {
		t.Fatalf("failed to unmarshal report.json: %v", err)
	}

	if execReport.TotalChunks != 1 {
		t.Errorf("expected 1 total chunk, got %d", execReport.TotalChunks)
	}
	if execReport.FailedChunks != 1 {
		t.Errorf("expected 1 failed chunk, got %d", execReport.FailedChunks)
	}
	if execReport.SuccessfulChunks != 0 {
		t.Errorf("expected 0 successful chunks, got %d", execReport.SuccessfulChunks)
	}
	if len(execReport.Chunks) != 1 {
		t.Fatalf("expected 1 chunk detail entry, got %d", len(execReport.Chunks))
	}
	if execReport.Chunks[0].Status != "failed" {
		t.Errorf("expected chunk status to be 'failed', got: %s", execReport.Chunks[0].Status)
	}
	if execReport.Chunks[0].Attempts != 4 { // 1 initial + 3 retries = 4
		t.Errorf("expected 4 attempts, got %d", execReport.Chunks[0].Attempts)
	}
	if execReport.Chunks[0].Retries != 3 {
		t.Errorf("expected 3 retries, got %d", execReport.Chunks[0].Retries)
	}
	if !strings.Contains(execReport.Chunks[0].ErrorMessage, "simulated invoke error") {
		t.Errorf("expected chunk error message to contain 'simulated invoke error', got: %s", execReport.Chunks[0].ErrorMessage)
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
	outputPath := updatedReport.ReportPath + ".output.txt"
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("expected output.txt to be created for early failure, but it does not exist")
	}
	defer os.RemoveAll(filepath.Dir(updatedReport.ReportPath)) // Cleanup report directory

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

