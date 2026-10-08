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
