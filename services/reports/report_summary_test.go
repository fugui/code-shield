package reports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
)

func TestBuildMetaDTOPreservesCoverageSummaryDTOFields(t *testing.T) {
	root := t.TempDir()
	summary := coverage.ExecutionSummary{
		CoverageState:      coverage.StatePartial,
		TotalChunks:        2,
		ProcessedChunks:    2,
		SuccessChunks:      1,
		FailedChunks:       1,
		MissingAssessments: []string{"unit-missing"},
		FailedFileDetails: []coverage.ExecutionFailure{{
			ChunkUID: "chunk-fail", ChunkName: "failed", FilePath: "fail.cpp",
			Stage: "analysis", ErrorClass: "timeout", ErrorMessage: "timed out",
		}},
		FailedChunkDetails: []coverage.ExecutionFailure{{
			ChunkUID: "chunk-fail", ChunkName: "failed", FilePath: "fail.cpp",
			Stage: "analysis", ErrorClass: "timeout", ErrorMessage: "timed out",
		}},
	}
	data, err := json.Marshal(map[string]any{"coverage_summary": summary})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "summary.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	report := &models.TaskReport{ID: 999, ReportPath: filepath.Join(root, "report.md")}
	got := BuildMetaDTO(report)
	if got.CoverageSummary == nil || got.CoverageSummary.CoverageState != coverage.StatePartial ||
		got.CoverageSummary.FailedChunks != 1 || len(got.CoverageSummary.FailedChunkDetails) != 1 ||
		len(got.CoverageSummary.MissingAssessments) != 1 {
		t.Fatalf("coverage summary fields incomplete: %+v", got.CoverageSummary)
	}
}

func TestParseSynthesisDiagnosticsPreservesAttemptHistory(t *testing.T) {
	summary := parseSynthesisDiagnostics(map[string]interface{}{
		"synthesis": map[string]interface{}{
			"status":                   "success",
			"attempts":                 2,
			"resource_id":              "synthesis-resource",
			"resource_failovers":       1,
			"driver_failovers":         2,
			"resource_chain":           []interface{}{"primary", "alternate"},
			"error_classes":            []interface{}{"idle_timeout", "none"},
			"queue_wait_ms":            []interface{}{10, 20},
			"attempt_duration_seconds": []interface{}{0.5, 1.5},
			"duration_seconds":         2.5,
			"error_message":            "",
		},
	})
	if summary == nil || summary.Attempts != 2 || summary.ResourceID != "synthesis-resource" ||
		summary.ResourceFailovers != 1 || summary.DriverFailovers != 2 ||
		len(summary.ResourceChain) != 2 || summary.ResourceChain[1] != "alternate" ||
		len(summary.ErrorClasses) != 2 || summary.ErrorClasses[0] != "idle_timeout" ||
		len(summary.QueueWaitMS) != 2 || summary.QueueWaitMS[1] != 20 ||
		len(summary.AttemptDurationSeconds) != 2 || summary.AttemptDurationSeconds[1] != 1.5 ||
		summary.DurationSeconds != 2.5 {
		t.Fatalf("synthesis diagnostics = %+v", summary)
	}
	if parseSynthesisDiagnostics(nil) != nil {
		t.Fatal("empty summary unexpectedly produced synthesis diagnostics")
	}
}
