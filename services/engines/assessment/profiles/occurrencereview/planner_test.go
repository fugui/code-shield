package occurrencereview

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
)

func TestBuildKeywordOccurrenceBundlesFromPlanner(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.cpp"), []byte("void start() {}\nvoid stop() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	units := []coverage.PlanUnit{
		{ID: "o1", Kind: coverage.PlanUnitKeywordOccurrence, Path: "a.cpp", StartLine: 1, EndLine: 1},
		{ID: "o2", Kind: coverage.PlanUnitKeywordOccurrence, Path: "a.cpp", StartLine: 2, EndLine: 2},
	}
	bundles, err := (BundlePlanner{}).BuildBundles(assessment.PlanContext{Config: engines.ChunkConfig{MaxFiles: 1}}, units)
	if err != nil {
		t.Fatalf("BuildBundles() error = %v", err)
	}
	if len(bundles) != 2 || bundles[0].PrimaryUnits[0].ID != "o1" || bundles[1].PrimaryUnits[0].ID != "o2" {
		t.Fatalf("unexpected bundles: %+v", bundles)
	}
}
