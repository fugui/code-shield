package profiles

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssessmentCoreDoesNotLeakBusinessProfileTerms(t *testing.T) {
	bannedTerms := []string{"not_thread_creation", "thread_create", "test_case"}
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve architecture test location")
	}
	testDir := filepath.Dir(testFile)
	coreRoots := []string{
		filepath.Join(testDir, "..", "..", "assessment"),
		filepath.Join(testDir, "..", "..", "debate"),
		filepath.Join(testDir, "..", "..", "..", "reports"),
		filepath.Join(testDir, "..", "..", "..", "governance"),
		filepath.Join(testDir, "..", "..", "..", "runner"),
		filepath.Join(testDir, "..", "..", "..", "queue"),
	}
	for _, root := range coreRoots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "profiles" && filepath.Base(filepath.Dir(path)) == "assessment" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, "_test.go") || !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, term := range bannedTerms {
				if strings.Contains(string(content), term) {
					t.Fatalf("%s contains assessment-core business term %q", path, term)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDebateEngineDelegatesSpecializedBundleConstruction(t *testing.T) {
	enginePath := filepath.Join("..", "..", "debate", "engine.go")
	source, err := os.ReadFile(enginePath)
	if err != nil {
		testDir, cwdErr := os.Getwd()
		if cwdErr != nil {
			t.Fatalf("read debate engine: %v; resolve cwd: %v", err, cwdErr)
		}
		t.Fatalf("read debate engine from %s: %v", testDir, err)
	}
	text := string(source)
	for _, forbidden := range []string{
		"isSpecializedProfile",
		"BuildChangeUnitBundles",
		"BuildPrimaryUnitBundles",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("debate engine leaked specialized bundle construction: %q", forbidden)
		}
	}
}
