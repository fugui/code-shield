package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBuildTracksChunkStatusesAndStableIDs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "ok.cpp"), []byte("int ok;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "fail.cpp"), []byte("int fail;"), 0644); err != nil {
		t.Fatal(err)
	}

	got := Build(root, "abc", "v1", "policy-x", []Chunk{
		{UID: "chunk-ok", Status: StatusSuccess, Files: []string{"src/ok.cpp"}},
		{UID: "chunk-fail", Status: StatusFailed, Files: []string{"src/fail.cpp"}, Error: "timeout"},
	})

	if len(got.Files) != 2 {
		t.Fatalf("expected 2 planned files, got %d", len(got.Files))
	}
	statuses := map[string]string{}
	for _, file := range got.Files {
		statuses[file.Path] = file.Status
	}
	if statuses["src/ok.cpp"] != StatusSuccess || statuses["src/fail.cpp"] != StatusFailed {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}

	summary := got.Summary()
	if summary.ScannedFiles != 1 || summary.FailedFiles != 1 || summary.SuccessChunks != 1 || summary.FailedChunks != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.CoverageComplete || !summary.CoverageDegraded {
		t.Fatalf("coverage should be degraded: %+v", summary)
	}
}

func TestBuildAggregatesMultiChunkFileStatuses(t *testing.T) {
	cases := []struct {
		name         string
		chunkStatus  []string
		wantStatus   string
		wantError    string
		wantErrClass string
	}{
		{
			name:        "all success chunks cover file",
			chunkStatus: []string{StatusSuccess, StatusSuccess, StatusSuccess},
			wantStatus:  StatusSuccess,
		},
		{
			name:         "failed chunk fails file",
			chunkStatus:  []string{StatusSuccess, StatusFailed, StatusSuccess},
			wantStatus:   StatusFailed,
			wantError:    "assessment failed",
			wantErrClass: "timeout",
		},
		{
			name:        "failed chunk wins over unknown chunks",
			chunkStatus: []string{StatusUnknown, StatusFailed, StatusSuccess},
			wantStatus:  StatusFailed,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "TestReticleStagePhyAppMaintenance.cpp")
			if err := os.WriteFile(path, []byte("int covered;"), 0644); err != nil {
				t.Fatal(err)
			}

			chunks := make([]Chunk, 0, len(test.chunkStatus))
			for idx, status := range test.chunkStatus {
				chunk := Chunk{
					UID:    fmt.Sprintf("entity-%03d", idx+28),
					Status: status,
					Files:  []string{"TestReticleStagePhyAppMaintenance.cpp"},
				}
				if status == StatusFailed {
					chunk.Error = test.wantError
					chunk.ErrorClass = test.wantErrClass
				}
				chunks = append(chunks, chunk)
			}

			got := Build(root, "abc", "v1", "policy-x", chunks)
			if len(got.Files) != 1 {
				t.Fatalf("expected 1 file, got %d", len(got.Files))
			}
			file := got.Files[0]
			if file.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", file.Status, test.wantStatus)
			}
			if file.ChunkUID != "" || file.ChunkSnapshotHash != "" {
				t.Fatalf("multi-chunk file must not claim a representative owner: %+v", file)
			}
			if file.Error != test.wantError || file.ErrorClass != test.wantErrClass {
				t.Fatalf("unexpected failure fields: error=%q errorClass=%q", file.Error, file.ErrorClass)
			}
		})
	}
}

func TestSummaryCompleteRequiresAllFacts(t *testing.T) {
	got := Coverage{
		PolicyVersion:    "v1",
		CommitHash:       "abc",
		CommitVerified:   true,
		WorktreeClean:    true,
		AnalysisComplete: true,
		Files: []File{
			{Path: "src/a.cpp", Status: StatusSuccess},
			{Path: "src/b.cpp", Status: StatusSuccess},
		},
	}
	summary := got.Summary()
	if !summary.CoverageComplete || summary.CoverageDegraded || summary.Degraded {
		t.Fatalf("expected complete coverage, got %+v", summary)
	}

	got.WorktreeClean = false
	summary = got.Summary()
	if summary.CoverageComplete || !summary.CoverageDegraded {
		t.Fatalf("dirty worktree must degrade coverage, got %+v", summary)
	}
}

func TestSummaryExcludedFilesDoNotBlockCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "scanned.cpp"), []byte("int ok;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "excluded.txt"), []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}

	got := Build(root, "abc", "v1", "policy-x", []Chunk{
		{UID: "chunk-ok", Status: StatusSuccess, Files: []string{"scanned.cpp"}},
		{
			UID:    "coverage-plan-excluded",
			Status: StatusExcluded,
			PlannedFiles: []PlannedFile{{
				Path:   "excluded.txt",
				Reason: ReasonNotSourceFile,
			}},
		},
	})
	got.CommitVerified = true
	got.WorktreeClean = true
	summary := got.Summary()
	if summary.ScannedFiles != 1 || summary.ExcludedFiles != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if got.Files[0].Status != StatusExcluded || got.Files[0].Reason != ReasonNotSourceFile {
		t.Fatalf("unexpected excluded fact: %+v", got.Files[1])
	}
	if !summary.CoverageComplete || summary.CoverageDegraded || summary.Degraded {
		t.Fatalf("expected complete coverage, got %+v", summary)
	}
}

func TestSummaryUnknownFilesDegradeCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.cpp"), []byte("int ok;"), 0644); err != nil {
		t.Fatal(err)
	}

	got := Build(root, "abc", "v1", "policy-x", []Chunk{
		{UID: "chunk-ok", Status: StatusSuccess, Files: []string{"ok.cpp"}},
		{
			UID:    "coverage-plan-unknown",
			Status: StatusUnknown,
			PlannedFiles: []PlannedFile{{
				Path:   "unreadable.cpp",
				Reason: ReasonContentFilterError,
			}},
		},
	})
	got.CommitVerified = true
	got.WorktreeClean = true
	summary := got.Summary()
	if summary.UnknownFiles != 1 || summary.ScannedFiles != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.CoverageComplete || !summary.CoverageDegraded {
		t.Fatalf("unknown primary scope must degrade coverage: %+v", summary)
	}
	if !slices.Contains(summary.CoverageReasons, "COVERAGE_UNKNOWN") {
		t.Fatalf("missing COVERAGE_UNKNOWN reason: %+v", summary.CoverageReasons)
	}
}

func TestSummaryStateMappingAndFailureItems(t *testing.T) {
	complete := Coverage{
		AnalysisComplete: true,
		CommitVerified:   true,
		WorktreeClean:    true,
		Files:            []File{{Path: "a.cpp", Status: StatusSuccess}},
	}.Summary()
	if !complete.CoverageComplete || complete.State() != StateComplete {
		t.Fatalf("complete summary state = %s, flags=%+v", complete.State(), complete)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.cpp"), []byte("int ok;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.cpp"), []byte("int fail;"), 0644); err != nil {
		t.Fatal(err)
	}
	degraded := Build(root, "abc", "v1", "policy-x", []Chunk{
		{UID: "chunk-ok", Status: StatusSuccess, Files: []string{"a.cpp"}},
		{UID: "chunk-fail", Status: StatusFailed, Files: []string{"b.cpp"}, Error: "timeout", ErrorClass: "timeout"},
	}).Summary()
	if degraded.FailedChunks != 1 || degraded.State() != StatePartial {
		t.Fatalf("degraded summary state=%s summary=%+v", degraded.State(), degraded)
	}
	if len(degraded.FailedFileItems) != 1 || len(degraded.FailedChunkItems) != 1 {
		t.Fatalf("failure items not aggregated: files=%d chunks=%d", len(degraded.FailedFileItems), len(degraded.FailedChunkItems))
	}
	if degraded.FailedFileItems[0].FilePath != "b.cpp" ||
		degraded.FailedFileItems[0].ErrorClass != "timeout" ||
		degraded.FailedChunkItems[0].ErrorClass != "timeout" {
		t.Fatalf("failure item fields incomplete: %+v", degraded.FailedFileItems[0])
	}

	manifestMissing := Coverage{ManifestMissing: true}.Summary()
	if manifestMissing.State() != StateFailed {
		t.Fatalf("missing manifest state = %s", manifestMissing.State())
	}
}

func TestTerminalTaskStatusMapsCoverageSSOT(t *testing.T) {
	cases := []struct {
		name  string
		value ExecutionSummary
		want  string
	}{
		{name: "success", value: ExecutionSummary{CoverageState: StateComplete}, want: "success"},
		{name: "not applicable", value: ExecutionSummary{CoverageState: StateNotApplicable}, want: "success"},
		{name: "partial", value: ExecutionSummary{CoverageState: StatePartial, CoverageDegraded: true}, want: "degraded"},
		{name: "failed chunks", value: ExecutionSummary{CoverageState: StatePartial, TotalChunks: 3, FailedChunks: 1}, want: "degraded"},
		{name: "missing assessments", value: ExecutionSummary{CoverageState: StateComplete, MissingAssessments: []string{"unit-1"}}, want: "degraded"},
		{name: "all chunks failed", value: ExecutionSummary{CoverageState: StatePartial, TotalChunks: 2, FailedChunks: 2}, want: "failed"},
		{name: "coverage failed", value: ExecutionSummary{CoverageState: StateFailed}, want: "failed"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := TerminalTaskStatus(test.value, false); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
	if got := TerminalTaskStatus(ExecutionSummary{CoverageState: StateComplete}, true); got != "failed" {
		t.Fatalf("critical stage status = %q, want failed", got)
	}
}

func TestSummaryUnchangedSkippedRemainsPartial(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "changed.cpp"), []byte("int changed;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unchanged.cpp"), []byte("int unchanged;"), 0644); err != nil {
		t.Fatal(err)
	}

	got := Build(root, "abc", "v1", "policy-x", []Chunk{
		{UID: "chunk-ok", Status: StatusSuccess, Files: []string{"changed.cpp"}},
		{
			UID:          "coverage-plan-unchanged-skipped",
			Status:       StatusUnchangedSkipped,
			PlannedFiles: []PlannedFile{{Path: "unchanged.cpp", DiffTouched: false}},
		},
	})
	summary := got.Summary()
	if summary.ScannedFiles != 1 || summary.UnchangedSkippedFiles != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.CoverageComplete || !summary.CoverageDegraded || !summary.Degraded {
		t.Fatalf("expected partial coverage, got %+v", summary)
	}
	hasReason := false
	for _, reason := range summary.CoverageReasons {
		if reason == "UNCHANGED_SKIPPED" {
			hasReason = true
		}
	}
	if !hasReason {
		t.Fatalf("expected unchanged skip reason, got %+v", summary.CoverageReasons)
	}
}

func TestSummaryWithoutPlannedFilesIsNotApplicable(t *testing.T) {
	got := Coverage{
		PolicyVersion:  "v1",
		CommitVerified: true,
		WorktreeClean:  true,
	}
	summary := got.Summary()
	if summary.PlannedFiles != 0 {
		t.Fatalf("unexpected planned files: %d", summary.PlannedFiles)
	}
	if !summary.CoverageNotApplicable || summary.NoScopeReason != ReasonNoPlannedFiles {
		t.Fatalf("expected no-scope coverage, got %+v", summary)
	}
	if summary.CoverageComplete || summary.CoverageDegraded || summary.Degraded {
		t.Fatalf("no-scope coverage must not be degraded: %+v", summary)
	}
}

func TestMissingManifestRemainsDegraded(t *testing.T) {
	got := Coverage{
		PolicyVersion:   "v1",
		CommitVerified:  true,
		WorktreeClean:   true,
		ManifestMissing: true,
	}
	summary := got.Summary()
	if summary.CoverageNotApplicable {
		t.Fatalf("missing manifest must not be no-scope: %+v", summary)
	}
	if summary.CoverageComplete || !summary.CoverageDegraded || !summary.Degraded {
		t.Fatalf("missing manifest must degrade coverage: %+v", summary)
	}
	if len(summary.CoverageReasons) != 1 || summary.CoverageReasons[0] != ReasonManifestMissing {
		t.Fatalf("unexpected coverage reasons: %#v", summary.CoverageReasons)
	}
}

func TestStableChunkIDIsDeterministic(t *testing.T) {
	first := StableChunkID("policy", []string{"src/b.cpp", "src/a.cpp"})
	second := StableChunkID("policy", []string{"src/a.cpp", "src/b.cpp"})
	if first == "" || first != second {
		t.Fatalf("expected stable chunk id, got %q and %q", first, second)
	}
}

func TestCoverageSummaryDegradesOnPlanDrift(t *testing.T) {
	got := Coverage{
		PolicyVersion:  "v1",
		CommitVerified: true,
		WorktreeClean:  true,
		PlanReconciliation: &PlanReconciliation{
			PlannedUnits:   2,
			MatchedUnits:   1,
			MissingUnits:   []string{"missing"},
			UnmatchedUnits: []string{"orphan"},
		},
		Files: []File{{Path: "src/a.cpp", Status: StatusSuccess, ChunkUID: "chunk-1"}},
	}
	summary := got.Summary()
	if summary.CoverageComplete || !summary.CoverageDegraded || !summary.Degraded {
		t.Fatalf("plan drift must degrade coverage: %+v", summary)
	}
	if !slices.Contains(summary.CoverageReasons, "MISSING_ASSESSMENT") ||
		!slices.Contains(summary.CoverageReasons, "UNMATCHED_ASSESSMENT") {
		t.Fatalf("missing plan drift reasons: %+v", summary.CoverageReasons)
	}
}
