package defectlifecycle

import (
	"testing"

	"code-common/backend/testdb"
	"code-shield/models"
	"code-shield/services/coverage"
	"time"
)

func TestBuildScopeEntriesMapsCoverageOutcomes(t *testing.T) {
	input := ScanInput{
		RepoRoot: "/tmp/repo",
		Repo:     models.Repository{ID: 1},
		TaskType: models.TaskType{ID: 2},
		Report:   models.TaskReport{ID: 3},
		Coverage: &coverage.Coverage{
			Files: []coverage.File{
				{Path: "src/scanned.cpp", Status: coverage.StatusSuccess, Hash: "a"},
				{Path: "src/failed.cpp", Status: coverage.StatusFailed, Error: "timeout"},
				{Path: "src/planned.cpp", Status: coverage.StatusPlanned},
				{Path: "src/excluded.txt", Status: coverage.StatusExcluded, Reason: coverage.ReasonNotSourceFile},
				{Path: "src/unchanged.cpp", Status: coverage.StatusUnchangedSkipped, DiffTouched: false},
				{Path: "src/diff.cpp", Status: coverage.StatusSuccess, DiffTouched: true, HunkRanges: []string{"1-2"}},
			},
		},
	}

	entries := buildScopeEntries(input)
	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(entries))
	}
	byPath := map[string]models.ScanScopeEntry{}
	for _, entry := range entries {
		byPath[entry.NormPath] = entry
	}
	expected := map[string]string{
		"src/scanned.cpp":   ScopeScanned,
		"src/failed.cpp":    ScopeFailed,
		"src/planned.cpp":   ScopePlanned,
		"src/excluded.txt":  ScopeExcluded,
		"src/unchanged.cpp": ScopeUnchangedSkipped,
		"src/diff.cpp":      ScopeScanned,
	}
	for path, outcome := range expected {
		if byPath[path].Outcome != outcome {
			t.Fatalf("expected %s=%s, got %+v", path, outcome, byPath[path])
		}
	}
	if byPath["src/failed.cpp"].FailReason != "timeout" {
		t.Fatalf("unexpected failed reason: %+v", byPath["src/failed.cpp"])
	}
	if byPath["src/excluded.txt"].FailReason != coverage.ReasonNotSourceFile {
		t.Fatalf("unexpected excluded reason: %+v", byPath["src/excluded.txt"])
	}
	if !byPath["src/diff.cpp"].DiffTouched || string(byPath["src/diff.cpp"].HunkRanges) != `["1-2"]` {
		t.Fatalf("unexpected diff facts: %+v", byPath["src/diff.cpp"])
	}
}

func TestBuildScopeEntriesDeduplicatesCaseFoldedPaths(t *testing.T) {
	input := ScanInput{
		RepoRoot: "/tmp/repo",
		Repo:     models.Repository{ID: 1},
		TaskType: models.TaskType{ID: 2},
		Report:   models.TaskReport{ID: 3},
		Coverage: &coverage.Coverage{
			Files: []coverage.File{
				{Path: "docs/README.md", Status: coverage.StatusExcluded, Reason: coverage.ReasonNotSourceFile},
				{Path: "docs/readme.md", Status: coverage.StatusExcluded, Reason: coverage.ReasonNotSourceFile},
				{Path: "src/failed.cpp", Status: coverage.StatusSuccess, Hash: "first"},
				{Path: "SRC/FAILED.cpp", Status: coverage.StatusFailed, ErrorClass: "timeout", Error: "timeout"},
			},
		},
	}

	entries := buildScopeEntries(input)
	if len(entries) != 2 {
		t.Fatalf("expected 2 unique entries, got %d: %+v", len(entries), entries)
	}
	byPath := map[string]models.ScanScopeEntry{}
	for _, entry := range entries {
		if _, exists := byPath[entry.NormPath]; exists {
			t.Fatalf("duplicate normalized path: %s", entry.NormPath)
		}
		byPath[entry.NormPath] = entry
	}
	if byPath["docs/readme.md"].Outcome != ScopeExcluded {
		t.Fatalf("unexpected excluded entry: %+v", byPath["docs/readme.md"])
	}
	failed := byPath["src/failed.cpp"]
	if failed.Outcome != ScopeFailed || failed.ErrorClass != "timeout" || failed.FailReason != "timeout" {
		t.Fatalf("failed diagnostics not merged: %+v", failed)
	}
}

func TestMergeChunkExecutionScopeEntriesUsesFinalFailedOutcome(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, "shield_scope_merge", &models.Repository{}, &models.TaskType{}, &models.TaskReport{}, &models.ScanScopeEntry{}, &models.TaskChunkExecution{})
	if db == nil {
		t.Skip("Database not available, skipping DB test")
	}
	now := time.Now()
	input := ScanInput{
		DB:       db,
		RepoRoot: "/tmp/repo",
		Repo:     models.Repository{ID: 1},
		TaskType: models.TaskType{ID: 2},
		Report:   models.TaskReport{ID: 3},
		Coverage: &coverage.Coverage{Files: []coverage.File{{
			Path: "src/failed.cpp", Status: coverage.StatusSuccess, Hash: "blob", ChunkUID: "coverage-uid",
		}}},
	}
	entries := buildScopeEntries(input)
	executions := []models.TaskChunkExecution{
		{ReportID: 3, RepoID: 1, TaskTypeID: 2, ChunkUID: "chunk-uid", ChunkName: "bundle", PrimaryUnitID: "unit", FilePath: "src/failed.cpp", Stage: "analysis", Attempt: 1, AttemptKind: "invocation", IsFinal: true, Status: models.StatusFailed, ErrorClass: "timeout", ErrorMessage: "first timeout", StartedAt: now, FinishedAt: &now},
		{ReportID: 3, RepoID: 1, TaskTypeID: 2, ChunkUID: "chunk-uid", ChunkName: "bundle", PrimaryUnitID: "unit", FilePath: "src/failed.cpp", Stage: "analysis", Attempt: 2, AttemptKind: "fresh_retry", IsFinal: false, Status: models.StatusFailed, ErrorClass: "rate_limited", ErrorMessage: "ignored retry", StartedAt: now, FinishedAt: &now},
	}
	for index := range executions {
		if err := db.Create(&executions[index]).Error; err != nil {
			t.Fatal(err)
		}
	}

	merged := mergeChunkExecutionScopeEntries(input, entries)
	if len(merged) != 1 || merged[0].Outcome != ScopeFailed {
		t.Fatalf("final failure did not update outcome: %+v", merged)
	}
	if merged[0].ChunkUID != "chunk-uid" || merged[0].PrimaryUnitID != "unit" ||
		merged[0].ErrorClass != "timeout" || merged[0].FailReason != "first timeout" {
		t.Fatalf("final attempt diagnostics not preserved: %+v", merged[0])
	}
}
