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

func TestBuildScanCoverageChunkUID(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.cpp", "b.cpp", "c.cpp"} {
		if err := os.WriteFile(filepath.Join(root, "src", f), []byte("int f() { return 0; }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	longName := "bundle-name-that-is-excessively-long-and-definitely-exceeds-the-maximum-allowed-sixty-four-characters-limit-in-the-database-schema"
	details := []ChunkDetails{
		{
			ChunkUID:  "stable-uid-12345",
			ChunkName: "human-readable-bundle-name",
			Status:    coverage.StatusSuccess,
			Files:     []string{"src/a.cpp"},
		},
		{
			ChunkUID:  "",
			ChunkName: "fallback-bundle-name",
			Status:    coverage.StatusSuccess,
			Files:     []string{"src/b.cpp"},
		},
		{
			ChunkUID:  "",
			ChunkName: longName,
			Status:    coverage.StatusSuccess,
			Files:     []string{"src/c.cpp"},
		},
	}

	built := BuildScanCoverage(&EngineContext{CodesPath: root}, details, nil)
	byPath := map[string]coverage.File{}
	for _, file := range built.Files {
		byPath[file.Path] = file
	}

	// 1. 优先使用 ChunkUID
	if byPath["src/a.cpp"].ChunkUID != "stable-uid-12345" {
		t.Fatalf("src/a.cpp ChunkUID = %q, want %q", byPath["src/a.cpp"].ChunkUID, "stable-uid-12345")
	}

	// 2. ChunkUID 为空时回退到 ChunkName
	if byPath["src/b.cpp"].ChunkUID != "fallback-bundle-name" {
		t.Fatalf("src/b.cpp ChunkUID = %q, want %q", byPath["src/b.cpp"].ChunkUID, "fallback-bundle-name")
	}

	// 3. ChunkName 超过 64 字符时确保截断且不超过 64
	cUID := byPath["src/c.cpp"].ChunkUID
	if len(cUID) > 64 {
		t.Fatalf("src/c.cpp ChunkUID length = %d > 64: %q", len(cUID), cUID)
	}
	if cUID != longName[:64] {
		t.Fatalf("src/c.cpp ChunkUID = %q, want %q", cUID, longName[:64])
	}
}
