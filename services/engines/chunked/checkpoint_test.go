package chunked

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
)

func TestChunkCheckpointWriteLoadValidateLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	chunkDir := filepath.Join(tempDir, "chunks-42-repo")
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("create chunk dir: %v", err)
	}

	sourcePath := filepath.Join(tempDir, "alpha.go")
	source := []byte("package alpha\n")
	if err := os.WriteFile(sourcePath, source, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	files := []string{"alpha.go"}
	req := engines.ChunkExecutionRequest{
		Index:      1,
		Name:       "alpha",
		Files:      files,
		OutputPath: ChunkOutputPath(chunkDir, 42, "alpha"),
	}
	result := engines.ChunkExecutionResult{
		Findings: []models.AnalysisFinding{{
			Title:    "alpha finding",
			FilePath: "alpha.go",
			Severity: "一般",
			Category: "质量违规",
		}},
		Attempts:              2,
		Retries:               1,
		ArtifactComplete:      boolPtr(true),
		ArtifactState:         "observed",
		NormalizedIssueCount:  2,
		SchemaRepairAttempts:  1,
		SchemaRepairSuccesses: 1,
	}
	ctx := ChunkCheckpointContext{
		ReportID:     42,
		EngineMode:   "chunked",
		CodesPath:    tempDir,
		EngineConfig: json.RawMessage(`{"max_files":1}`),
		PromptPath:   "",
	}

	if err := WriteChunkCheckpoint(ctx, req, result); err != nil {
		t.Fatalf("WriteChunkCheckpoint() error = %v", err)
	}

	metadata := BuildChunkResumeMetadata(tempDir, ctx.EngineConfig, ctx.PromptPath)
	snapshotHash, err := ChunkSnapshotHash(tempDir, files)
	if err != nil {
		t.Fatalf("ChunkSnapshotHash() error = %v", err)
	}

	loaded := LoadChunkResumeCheckpoints(chunkDir)
	checkpoint, exists := loaded["alpha"]
	if !exists {
		t.Fatal("checkpoint for alpha was not loaded")
	}
	if !ValidateChunkResumeCheckpoint(checkpoint, 42, "chunked", "alpha", files, metadata, snapshotHash) {
		t.Fatal("checkpoint should be valid")
	}
	if checkpoint.Result.Attempts != 2 || checkpoint.Result.Retries != 1 {
		t.Fatalf("checkpoint metrics = (%d, %d), want (2, 1)",
			checkpoint.Result.Attempts, checkpoint.Result.Retries)
	}
	if checkpoint.Result.ArtifactComplete == nil || !*checkpoint.Result.ArtifactComplete ||
		checkpoint.Result.ArtifactState != "observed" ||
		checkpoint.Result.NormalizedIssueCount != 2 ||
		checkpoint.Result.SchemaRepairSuccesses != 1 {
		t.Fatalf("artifact state not persisted: %+v", checkpoint.Result)
	}
	if len(checkpoint.Result.Findings) != 1 || checkpoint.Result.Findings[0].Title != "alpha finding" {
		t.Fatalf("unexpected checkpoint findings: %+v", checkpoint.Result.Findings)
	}

	if err := os.WriteFile(sourcePath, []byte("package alpha // changed\n"), 0644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	changedHash, err := ChunkSnapshotHash(tempDir, files)
	if err != nil {
		t.Fatalf("changed snapshot hash error = %v", err)
	}
	if ValidateChunkResumeCheckpoint(checkpoint, 42, "chunked", "alpha", files, metadata, changedHash) {
		t.Fatal("checkpoint must be invalid after source content changes")
	}
}

func TestSafeChunkNameMatchesOutputPathConvention(t *testing.T) {
	if got := SafeChunkName("src/sub dir/a.cc"); got != "src-sub-dir-a-cc" {
		t.Fatalf("SafeChunkName() = %q, want %q", got, "src-sub-dir-a-cc")
	}
}

func TestChunkCheckpointRejectsV1(t *testing.T) {
	tempDir := t.TempDir()
	chunkDir := filepath.Join(tempDir, "chunks-1-repo")
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tempDir, "alpha.go")
	if err := os.WriteFile(source, []byte("package alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := []string{"alpha.go"}
	hash, err := ChunkSnapshotHash(tempDir, files)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ChunkCheckpoint{
		SchemaVersion:    1,
		ReportID:         1,
		EngineMode:       "chunked",
		ChunkName:        "alpha",
		Files:            files,
		SnapshotHash:     hash,
		EngineConfigHash: "config",
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ChunkCheckpointPath(chunkDir, 1, "alpha"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if len(LoadChunkResumeCheckpoints(chunkDir)) != 0 {
		t.Fatal("v1 checkpoint must be rejected by default")
	}
}

func boolPtr(value bool) *bool {
	return &value
}
