package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
)

func TestLoadExecutionSnapshotKeepsPromptContentInMemory(t *testing.T) {
	tempDir := t.TempDir()
	oldDataDir := models.AppConfig.Server.DataDir
	models.AppConfig.Server.DataDir = tempDir
	t.Cleanup(func() { models.AppConfig.Server.DataDir = oldDataDir })

	promptContent := "immutable domain prompt"
	snapshotRaw, err := json.Marshal(models.ExecutionContextSnapshot{
		EngineMode:        "debate_full",
		PromptContent:     promptContent,
		PromptContentHash: "prompt-hash",
	})
	if err != nil {
		t.Fatalf("marshal execution snapshot: %v", err)
	}
	ctx := &TaskContext{Report: models.TaskReport{
		ID:                     1,
		EngineMode:             "debate_full",
		PromptContentHash:      "prompt-hash",
		ExecutionSnapshot:      snapshotRaw,
		ExecutionSnapshotState: "complete",
	}}

	if err := ctx.LoadExecutionSnapshot(); err != nil {
		t.Fatalf("LoadExecutionSnapshot() error = %v", err)
	}
	if ctx.PromptContent != promptContent || ctx.PromptContentHash != "prompt-hash" {
		t.Fatalf("prompt content not retained: %+v", ctx)
	}
	if ctx.PromptPath != "" {
		t.Fatalf("PromptPath = %q, want empty", ctx.PromptPath)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "reports", "prompt-snapshot.md")); !os.IsNotExist(err) {
		t.Fatalf("prompt-snapshot.md should not be materialized, stat error = %v", err)
	}
}

func TestBuildCoverageExecutionSummaryAggregatesFacts(t *testing.T) {
	ctx := &TaskContext{
		Report: models.TaskReport{ID: 1},
		Coverage: &coverage.Coverage{
			CommitVerified:   true,
			WorktreeClean:    true,
			AnalysisComplete: true,
			PlanReconciliation: &coverage.PlanReconciliation{
				PlannedUnits: 1,
				MatchedUnits: 0,
				MissingUnits: []string{"unit-missing"},
			},
			Files: []coverage.File{
				{Path: "ok.cpp", Status: coverage.StatusSuccess, ChunkUID: "chunk-ok"},
				{Path: "fail.cpp", Status: coverage.StatusFailed, ChunkUID: "chunk-fail", Error: "timeout"},
			},
		},
		Summary: TaskSummaryReport{Analysis: AnalysisSummary{
			TotalChunks:   2,
			SuccessChunks: 1,
			FailedChunks:  1,
			Chunks: []ChunkDetails{{
				ChunkUID:      "chunk-fail",
				ChunkName:     "failed display",
				PrimaryUnitID: "unit-1",
				Files:         []string{"fail.cpp"},
				Status:        models.StatusFailed,
				ErrorClass:    "timeout",
				ErrorMessage:  "provider timed out",
			}},
		}},
	}

	got := buildCoverageExecutionSummary(ctx)
	if got.CoverageState != coverage.StatePartial || got.FailedChunks != 1 || got.SuccessChunks != 1 {
		t.Fatalf("unexpected aggregate: %+v", got)
	}
	if len(got.MissingAssessments) != 1 || got.MissingAssessments[0] != "unit-missing" {
		t.Fatalf("missing assessments = %#v", got.MissingAssessments)
	}
	if len(got.FailedChunkDetails) != 1 || got.FailedChunkDetails[0].ChunkName != "failed display" ||
		got.FailedChunkDetails[0].FilePath != "fail.cpp" || got.FailedChunkDetails[0].ErrorClass != "timeout" {
		t.Fatalf("failed chunk detail incomplete: %+v", got.FailedChunkDetails)
	}
	if len(got.FailedFileDetails) != 1 || got.FailedFileDetails[0].ChunkName != "failed display" ||
		got.FailedFileDetails[0].PrimaryUnitID != "unit-1" || got.FailedFileDetails[0].ErrorClass != "timeout" {
		t.Fatalf("failed file detail not enriched: %+v", got.FailedFileDetails)
	}
}

func TestConvertEngineChunkDetailsPreservesStableIdentity(t *testing.T) {
	input := []engines.ChunkDetails{{
		ChunkUID:                "chunk-uid",
		ChunkName:               "bundle",
		PrimaryUnitID:           "unit-id",
		Files:                   []string{"src/a.cpp"},
		Status:                  models.StatusFailed,
		ErrorClass:              "timeout",
		ErrorMessage:            "timeout",
		ResourceFailovers:       1,
		DriverFailovers:         2,
		ResourceChain:           []string{"primary", "alternate"},
		ErrorClasses:            []string{"resource_unavailable", "none"},
		QueueWaitMS:             []int64{10, 20},
		AttemptDurationSeconds:  []float64{0.5, 1.5},
		ArtifactSchemaID:        "schema-id",
		ArtifactSchemaHash:      "sha256:schema",
		ResponseFormatMode:      "json_object",
		ResponseFormatFallbacks: 1,
	}}

	got := ConvertEngineChunkDetails(input)
	if len(got) != 1 || got[0].ChunkUID != input[0].ChunkUID ||
		got[0].PrimaryUnitID != input[0].PrimaryUnitID || got[0].ErrorClass != input[0].ErrorClass {
		t.Fatalf("stable chunk identity not preserved: %+v", got)
	}
	if got[0].ResourceFailovers != 1 || got[0].DriverFailovers != 2 ||
		len(got[0].ResourceChain) != 2 || len(got[0].ErrorClasses) != 2 ||
		len(got[0].QueueWaitMS) != 2 || len(got[0].AttemptDurationSeconds) != 2 {
		t.Fatalf("recovery observability not preserved: %+v", got[0])
	}
	if got[0].ArtifactSchemaID != "schema-id" || got[0].ArtifactSchemaHash != "sha256:schema" ||
		got[0].ResponseFormatMode != "json_object" || got[0].ResponseFormatFallbacks != 1 {
		t.Fatalf("native schema observability not preserved: %+v", got[0])
	}
}
