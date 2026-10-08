package debate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupSplitArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "hunter.json")
	kept := filepath.Join(tempDir, "hunter.split-1.json")
	removed := filepath.Join(tempDir, "hunter.split-2.json")
	unrelated := filepath.Join(tempDir, "hunter-other.json")

	for _, path := range []string{outPath, kept, removed, unrelated} {
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	cleanupSplitArtifacts(outPath)
	for _, path := range []string{outPath, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
	for _, path := range []string{kept, removed} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected split artifact %s to be removed, err=%v", path, err)
		}
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("expected split artifact to be removed, err=%v", err)
	}
}
