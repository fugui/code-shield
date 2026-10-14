package chunker

import (
	"strings"
	"testing"

	"code-shield/services/coverage"
)

func TestBuildPrimaryUnitBundlesGroupsAndSplitsByFile(t *testing.T) {
	units := []coverage.PlanUnit{
		{ID: "e-12", Kind: coverage.PlanUnitEntity, Path: "a_test.go", StartLine: 120},
		{ID: "e-01", Kind: coverage.PlanUnitEntity, Path: "a_test.go", StartLine: 10},
		{ID: "o-01", Kind: coverage.PlanUnitKeywordOccurrence, Path: "b.cpp", StartLine: 20},
	}
	bundles, err := BuildPrimaryUnitBundles(t.TempDir(), units, 1, nil)
	if err != nil {
		t.Fatalf("BuildPrimaryUnitBundles() error = %v", err)
	}
	if len(bundles) != 3 {
		t.Fatalf("expected 3 bundles, got %d", len(bundles))
	}
	if bundles[0].PrimaryUnits[0].ID != "e-01" || bundles[1].PrimaryUnits[0].ID != "e-12" ||
		bundles[2].PrimaryUnits[0].ID != "o-01" {
		t.Fatalf("unexpected bundle order: %+v", bundles)
	}
}

func TestBuildPrimaryUnitBundlesRejectsInvalidIdentity(t *testing.T) {
	_, err := BuildPrimaryUnitBundles(t.TempDir(), []coverage.PlanUnit{{ID: "e-1"}}, 1, nil)
	if err == nil || !strings.Contains(err.Error(), "empty path or id") {
		t.Fatalf("error = %v, want invalid identity", err)
	}
}

func TestSplitPrimaryUnitsBundleInHalf(t *testing.T) {
	units := make([]coverage.PlanUnit, 8)
	for i := 0; i < 8; i++ {
		units[i] = coverage.PlanUnit{
			ID:        strings.Repeat("u", i+1),
			Kind:      coverage.PlanUnitEntity,
			Path:      "pkg/test_suite.go",
			StartLine: (i + 1) * 10,
		}
	}
	parent := SemanticBundle{
		Name:         "entity-001",
		PrimaryFiles: []string{"pkg/test_suite.go"},
		HeaderFiles:  []string{"pkg/test_suite.h"},
		AllFiles:     []string{"pkg/test_suite.go", "pkg/test_suite.h"},
		PrimaryUnits: units,
	}

	halves := SplitSemanticBundleInHalf(parent)
	if len(halves) != 2 {
		t.Fatalf("expected 2 halves, got %d", len(halves))
	}
	if len(halves[0].PrimaryUnits) != 4 || len(halves[1].PrimaryUnits) != 4 {
		t.Fatalf("expected 4 units each, got %d and %d", len(halves[0].PrimaryUnits), len(halves[1].PrimaryUnits))
	}
	if halves[0].Name != "entity-001-split1" || halves[1].Name != "entity-001-split2" {
		t.Fatalf("unexpected names: %s, %s", halves[0].Name, halves[1].Name)
	}
	if len(halves[0].AllFiles) != 2 || halves[0].AllFiles[0] != "pkg/test_suite.go" || halves[0].AllFiles[1] != "pkg/test_suite.h" {
		t.Fatalf("unexpected AllFiles in half 1: %+v", halves[0].AllFiles)
	}
	if len(halves[1].AllFiles) != 2 || halves[1].AllFiles[0] != "pkg/test_suite.go" || halves[1].AllFiles[1] != "pkg/test_suite.h" {
		t.Fatalf("unexpected AllFiles in half 2: %+v", halves[1].AllFiles)
	}
	if halves[0].PrimaryUnits[0].ID != "u" || halves[1].PrimaryUnits[0].ID != "uuuuu" {
		t.Fatalf("unexpected unit splits: %s, %s", halves[0].PrimaryUnits[0].ID, halves[1].PrimaryUnits[0].ID)
	}
}
