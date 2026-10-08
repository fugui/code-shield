package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/invoker"
)

type retryCountingInvoker struct {
	failures int
	paths    []string
}

func (m *retryCountingInvoker) Name() string { return "retry-counting" }

func (m *retryCountingInvoker) Invoke(req invoker.AIRequest) error {
	m.paths = append(m.paths, req.OutputPath)
	if m.failures > 0 {
		m.failures--
		return os.ErrNotExist
	}
	return os.WriteFile(req.OutputPath, []byte(`{"findings":[],"summary":"ok"}`), 0644)
}

func TestExecuteChunkAnalysisCountsAttemptsAndIsolatesOutput(t *testing.T) {
	tempDir := t.TempDir()
	parentJSONPath := filepath.Join(tempDir, "parent.json")
	chunkJSONPath := filepath.Join(tempDir, "chunks", "chunk-42-alpha.json")

	backend := "retry-counting-backend"
	ai := &retryCountingInvoker{failures: 1}
	invoker.RegisterAIInvoker(backend, ai)

	oldTier1 := models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource: backend,
	}
	t.Cleanup(func() {
		models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = oldTier1
	})

	ctx := &TaskContext{
		Ctx:           context.Background(),
		Report:        models.TaskReport{ID: 42},
		TaskType:      models.TaskType{Timeout: 1},
		CodesPath:     tempDir,
		JsonPath:      parentJSONPath,
		ChunkPolicyID: "policy-x",
	}

	result, err := ExecuteChunkAnalysis(ctx, engines.ChunkExecutionRequest{
		Index:      1,
		Name:       "alpha",
		Files:      []string{"alpha/main.go"},
		OutputPath: chunkJSONPath,
	})
	if err != nil {
		t.Fatalf("ExecuteChunkAnalysis() error = %v", err)
	}
	if result.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", result.Attempts)
	}
	if result.Retries != 1 {
		t.Fatalf("Retries = %d, want 1", result.Retries)
	}
	if result.ErrorClass != string(invoker.ErrorClassNone) {
		t.Fatalf("ErrorClass = %q, want %q", result.ErrorClass, invoker.ErrorClassNone)
	}
	if result.OutputPath != chunkJSONPath {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath, chunkJSONPath)
	}
	if result.ChunkUID != coverage.StableChunkID("policy-x", []string{"alpha/main.go"}) {
		t.Fatalf("ChunkUID = %q, want coverage stable chunk ID", result.ChunkUID)
	}
	for _, path := range ai.paths {
		if path != chunkJSONPath+".raw" {
			t.Fatalf("attempt used output path %q, want %q", path, chunkJSONPath+".raw")
		}
	}
	if _, err := os.Stat(parentJSONPath); !os.IsNotExist(err) {
		t.Fatalf("parent JSON path unexpectedly exists: %v", err)
	}
	if ctx.Attempts != 0 {
		t.Fatalf("parent context Attempts = %d, want 0", ctx.Attempts)
	}

	if err := os.MkdirAll(filepath.Join(tempDir, "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "alpha", "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	built := coverage.Build(tempDir, "", "v1", "policy-x", []coverage.Chunk{{
		UID:    coverage.StableChunkID("policy-x", []string{"alpha/main.go"}),
		Status: coverage.StatusSuccess,
		Files:  []string{"alpha/main.go"},
	}})
	if len(built.Files) != 1 || built.Files[0].ChunkUID != result.ChunkUID {
		t.Fatalf("coverage ChunkUID = %q, want execution ChunkUID %q", built.Files[0].ChunkUID, result.ChunkUID)
	}
}
