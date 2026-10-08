package engines

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/coverage"
)

func TestBuildScanCoverageKeepsDeletedChangePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "session.cpp"), []byte("int session() { return 1; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	details := []ChunkDetails{
		{
			ChunkName: "change-001-src/session.cpp",
			Status:    coverage.StatusSuccess,
			Files:     []string{"src/session.cpp"},
			PlannedFiles: []coverage.PlannedFile{{
				Path:        "src/session.cpp",
				DiffTouched: true,
				HunkRanges:  []string{"1"},
			}},
		},
		{
			ChunkName: "change-002-src/legacy.cpp",
			Status:    coverage.StatusSuccess,
			PlannedFiles: []coverage.PlannedFile{{
				Path:        "src/legacy.cpp",
				DiffTouched: true,
				Deleted:     true,
				HunkRanges:  []string{"1-3"},
			}},
		},
	}

	built := BuildScanCoverage(&EngineContext{CodesPath: root}, details, nil)
	if len(built.Files) != 2 {
		t.Fatalf("coverage files = %d, want 2: %+v", len(built.Files), built.Files)
	}
	byPath := map[string]coverage.File{}
	for _, file := range built.Files {
		byPath[file.Path] = file
	}
	present := byPath["src/session.cpp"]
	if present.Status != coverage.StatusSuccess || !present.DiffTouched || len(present.HunkRanges) != 1 || present.HunkRanges[0] != "1" {
		t.Fatalf("unexpected present file coverage: %+v", present)
	}
	deleted := byPath["src/legacy.cpp"]
	if deleted.Status != coverage.StatusSuccess || !deleted.DiffTouched ||
		len(deleted.HunkRanges) != 1 || deleted.HunkRanges[0] != "1-3" {
		t.Fatalf("unexpected deleted file coverage: %+v", deleted)
	}
}
