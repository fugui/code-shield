package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
)

func TestIsBundleResumeEngineOnlySupportsDebateFull(t *testing.T) {
	if !IsBundleResumeEngine("debate_full") {
		t.Fatal("debate_full should support bundle resume")
	}
	for _, mode := range []string{"debate_selective", "chunked_fast", "chunked", "single"} {
		if IsBundleResumeEngine(mode) {
			t.Fatalf("%s must not claim bundle resume", mode)
		}
	}
}

func TestFindBundleResumeArtifactsBatch(t *testing.T) {
	dataRoot := t.TempDir()
	reportsRoot := filepath.Join(dataRoot, "reports")
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = dataRoot
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	writeCheckpoint := func(reportID uint) {
		chunkDir := filepath.Join(reportsRoot, "batch-review", "2026-01-02",
			fmt.Sprintf("debate-chunks-%d-repo", reportID))
		if err := os.MkdirAll(chunkDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(chunkDir, "resume-3.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeCheckpoint(101)
	writeCheckpoint(102)

	missingDir := filepath.Join(reportsRoot, "batch-review", "2026-01-03", "debate-chunks-103-repo")
	if err := os.MkdirAll(missingDir, 0755); err != nil {
		t.Fatal(err)
	}

	found := FindBundleResumeArtifacts("batch-review", "debate_full", []uint{101, 102, 103, 104})
	if !found[101] || !found[102] {
		t.Fatalf("expected checkpoints for 101 and 102, got %v", found)
	}
	if found[103] || found[104] {
		t.Fatalf("did not expect checkpoints for reports without resume files, got %v", found)
	}

	found = FindBundleResumeArtifacts("batch-review", "chunked", []uint{101})
	if len(found) != 0 {
		t.Fatalf("non-bundle engine must not report resume artifacts, got %v", found)
	}
}

func TestResumeChunkedTaskWithoutBundleCheckpointRequiresRescan(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_resume_requires_rescan",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "resume-demo", URL: "https://example.com/resume-demo.git"}
	taskType := models.TaskType{Name: "resume-review", DisplayName: "Resume Review", EngineMode: "debate_full"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusRunning}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}

	err := ResumeChunkedTask(report.ID)
	if !errors.Is(err, ErrResumeRequiresRescan) {
		t.Fatalf("ResumeChunkedTask() error = %v, want ErrResumeRequiresRescan", err)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.StatusFailed {
		t.Fatalf("status = %q, want %q", updated.Status, models.StatusFailed)
	}
	if !strings.Contains(updated.AISummary, "RESUME_REQUIRES_RESCAN") {
		t.Fatalf("summary does not contain rescan reason: %q", updated.AISummary)
	}
}

func TestResumeChunkedTaskRejectsLegacyEngineMode(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_runner_resume_legacy_mode",
		&models.Repository{}, &models.TaskType{}, &models.TaskReport{},
	)
	if db == nil {
		t.Skip("Database not available, skipping DB test")
		return
	}
	oldDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = oldDB })

	repo := models.Repository{Name: "legacy-resume-demo", URL: "https://example.com/legacy-resume-demo.git"}
	taskType := models.TaskType{Name: "legacy-resume-review", DisplayName: "Legacy Resume Review", EngineMode: "chunked"}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&taskType).Error; err != nil {
		t.Fatal(err)
	}
	report := models.TaskReport{RepoID: repo.ID, TaskTypeID: taskType.ID, Status: models.StatusRunning}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}

	err := ResumeChunkedTask(report.ID)
	if err == nil || !strings.Contains(err.Error(), "ENGINE_MODE_INVALID") {
		t.Fatalf("ResumeChunkedTask() error = %v, want ENGINE_MODE_INVALID", err)
	}

	var updated models.TaskReport
	if err := db.First(&updated, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.StatusFailed {
		t.Fatalf("status = %q, want %q", updated.Status, models.StatusFailed)
	}
}
