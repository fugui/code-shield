package changereview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/assessment"
)

func TestBuildChangeUnitBundlesGroupsSplitsAndProducesPluginPlan(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "int one() {\n  return 1;\n}\n\nint two() {\n  return 2;\n}\n\nint three() {\n  return 3;\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src/session.cpp"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	evidence := json.RawMessage(`{"anchor":{"path":"src/session.cpp"}}`)
	units := []coverage.PlanUnit{
		{ID: "h3", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 7, EndLine: 9, Evidence: evidence},
		{ID: "h1", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 1, EndLine: 3, Evidence: evidence},
		{ID: "h2", Kind: coverage.PlanUnitChangeHunk, Path: "src/session.cpp", StartLine: 4, EndLine: 6, Evidence: evidence},
	}
	planner := BundlePlanner{}
	bundles, err := planner.BuildBundles(assessment.PlanContext{Config: engines.ChunkConfig{MaxFiles: 2}}, units)
	if err != nil {
		t.Fatalf("BuildBundles() error = %v", err)
	}
	if len(bundles) != 2 || bundles[0].PrimaryUnits[0].ID != "h1" || bundles[1].PrimaryUnits[0].ID != "h3" {
		t.Fatalf("unexpected bundles: %+v", bundles)
	}
	plan := planner.PluginPlan(assessment.PlanContext{EngineContext: &engines.EngineContext{CodesPath: root}}, bundles[0])
	if len(plan) != 2 || plan[0].Path != "src/session.cpp" || plan[0].HunkRanges[0] != "1-3" ||
		plan[1].HunkRanges[0] != "4-6" {
		t.Fatalf("unexpected plugin plan: %+v", plan)
	}
}
