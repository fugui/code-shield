package chunker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/services/coverage"
)

func TestBuildChangeUnitBundlesGroupsSplitsAndKeepsDeletedPaths(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "src", "session.cpp"), []byte("int session() { return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	evidence, err := json.Marshal(map[string]any{
		"anchor": map[string]any{
			"path":        "src/session.cpp",
			"base_commit": "base",
			"head_commit": "head",
			"hunk_id":     "src/session.cpp#h1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	units := []coverage.PlanUnit{
		{ID: "src/session.cpp#h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 3, Evidence: evidence},
		{ID: "src/session.cpp#h2", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 1, Evidence: evidence},
		{ID: "src/session.cpp#h3", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 2, Evidence: evidence},
		{ID: "src/legacy.cpp#h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/legacy.cpp", StartLine: 1, Evidence: evidence},
	}

	bundles, err := BuildChangeUnitBundles(tmpDir, units, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 3 {
		t.Fatalf("bundle count = %d, want 3; names=%v", len(bundles), bundleDisplayNames(bundles))
	}

	presentBundles := make([]SemanticBundle, 0, 2)
	deletedBundles := make([]SemanticBundle, 0)
	for _, bundle := range bundles {
		if strings.Contains(bundle.Name, "src/legacy.cpp") {
			deletedBundles = append(deletedBundles, bundle)
			continue
		}
		presentBundles = append(presentBundles, bundle)
	}
	if len(presentBundles) != 2 || len(deletedBundles) != 1 {
		t.Fatalf("unexpected bundle split: present=%d deleted=%d", len(presentBundles), len(deletedBundles))
	}
	if presentBundles[0].PrimaryUnits[0].ID != "src/session.cpp#h2" ||
		presentBundles[0].PrimaryUnits[1].ID != "src/session.cpp#h3" ||
		presentBundles[1].PrimaryUnits[0].ID != "src/session.cpp#h1" {
		t.Fatalf("units were not sorted and split deterministically: %+v", presentBundles)
	}
	if len(deletedBundles[0].PrimaryUnits) != 1 {
		t.Fatalf("deleted bundle units = %d, want 1", len(deletedBundles[0].PrimaryUnits))
	}
	if len(deletedBundles[0].PrimaryFiles) != 1 || deletedBundles[0].PrimaryFiles[0] != "src/legacy.cpp" {
		t.Fatalf("deleted path excluded from primary files: %+v", deletedBundles[0])
	}
	for _, bundle := range bundles {
		for _, path := range bundle.AllFiles {
			if path == "src/legacy.cpp" {
				t.Fatalf("deleted path included in AllFiles: %+v", bundle)
			}
		}
	}
}

func TestBuildChangeUnitBundlesRejectsInvalidUnits(t *testing.T) {
	evidence := json.RawMessage(`{"anchor":{"base_commit":"base","head_commit":"head"}}`)
	tests := []struct {
		name   string
		units  []coverage.PlanUnit
		reason string
	}{
		{
			name:   "wrong unit kind",
			units:  []coverage.PlanUnit{{ID: "src/a.cpp", Path: "src/a.cpp", Evidence: evidence}},
			reason: "invalid change primary unit",
		},
		{
			name:   "missing evidence",
			units:  []coverage.PlanUnit{{ID: "src/a.cpp#h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/a.cpp"}},
			reason: "has no evidence pack",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildChangeUnitBundles(t.TempDir(), test.units, 2, nil)
			if err == nil || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("error = %v, want contains %q", err, test.reason)
			}
		})
	}
}

func bundleDisplayNames(bundles []SemanticBundle) []string {
	names := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		names = append(names, bundle.Name)
	}
	return names
}
