package debate

import (
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines/chunker"
)

func TestFileAssessmentRecordsUsesAllFiles(t *testing.T) {
	bundle := chunker.SemanticBundle{
		PrimaryFiles: []string{"src/a.cpp", "", "src/b.cpp"},
		HeaderFiles:  []string{"include/a.hpp"},
		AllFiles:     []string{"src/a.cpp", "src/b.cpp", "include/a.hpp"},
	}

	records := fileAssessmentRecords(bundle)
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}
	if records[0].PrimaryUnitID != "src/a.cpp" || records[1].PrimaryUnitID != "src/b.cpp" || records[2].PrimaryUnitID != "include/a.hpp" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestFullReviewReconciliationCoversMissingAndUnmatchedUnits(t *testing.T) {
	units := []coverage.PlanUnit{
		{ID: "src/a.cpp", Kind: coverage.PlanUnitFile, Path: "src/a.cpp"},
		{ID: "src/b.cpp", Kind: coverage.PlanUnitFile, Path: "src/b.cpp"},
	}
	assessments := []coverage.AssessmentRecord{
		{PrimaryUnitID: "src/a.cpp", Status: "assessed"},
		{PrimaryUnitID: "src/unknown.cpp", Status: "assessed"},
	}

	reconciliation := coverage.ReconcilePlanUnits(units, assessments)
	if reconciliation.PlannedUnits != 2 || reconciliation.MatchedUnits != 1 {
		t.Fatalf("unexpected planned/matched counts: %+v", reconciliation)
	}
	if len(reconciliation.MissingUnits) != 1 || reconciliation.MissingUnits[0] != "src/b.cpp" {
		t.Fatalf("unexpected missing units: %+v", reconciliation.MissingUnits)
	}
	if len(reconciliation.UnmatchedUnits) != 1 || reconciliation.UnmatchedUnits[0] != "src/unknown.cpp" {
		t.Fatalf("unexpected unmatched units: %+v", reconciliation.UnmatchedUnits)
	}
}
