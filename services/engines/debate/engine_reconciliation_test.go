package debate

import (
	"testing"

	"code-shield/models"
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

func TestReconcileLegacyBundleAssessmentsFillsCleanUnits(t *testing.T) {
	bundle := chunker.SemanticBundle{
		Name: "diff-bundle",
		PrimaryUnits: []coverage.PlanUnit{
			{ID: "hunk-1", Kind: coverage.PlanUnitChangeHunk, Path: "src/a.go"},
			{ID: "hunk-2", Kind: coverage.PlanUnitChangeHunk, Path: "src/a.go"},
			{ID: "hunk-3", Kind: coverage.PlanUnitChangeHunk, Path: "src/b.go"},
		},
	}

	// 场景 1: 部分单元有缺陷，未命中缺陷的单元被补齐为 clean/pass
	findings := []models.AnalysisFinding{
		{
			PrimaryUnitID:    "hunk-1",
			AssessmentStatus: "defect",
		},
	}
	records := reconcileLegacyBundleAssessments(bundle, findings)
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(records))
	}
	recordMap := make(map[string]string)
	for _, r := range records {
		recordMap[r.PrimaryUnitID] = r.Status
	}
	if recordMap["hunk-1"] != "defect" {
		t.Fatalf("hunk-1 status = %q, want defect", recordMap["hunk-1"])
	}
	if recordMap["hunk-2"] != "pass" {
		t.Fatalf("hunk-2 status = %q, want pass", recordMap["hunk-2"])
	}
	if recordMap["hunk-3"] != "pass" {
		t.Fatalf("hunk-3 status = %q, want pass", recordMap["hunk-3"])
	}

	// 场景 2: 全无缺陷的分片（0 findings），所有 primary units 均成功补齐
	cleanRecords := reconcileLegacyBundleAssessments(bundle, nil)
	if len(cleanRecords) != 3 {
		t.Fatalf("len(cleanRecords) = %d, want 3", len(cleanRecords))
	}
	for _, r := range cleanRecords {
		if r.Status != "pass" {
			t.Fatalf("clean record status for %s = %q, want pass", r.PrimaryUnitID, r.Status)
		}
	}

	// 场景 3: 对账验证，补齐后不产生 MissingUnits
	reconcile := coverage.ReconcilePlanUnits(bundle.PrimaryUnits, cleanRecords)
	if len(reconcile.MissingUnits) != 0 {
		t.Fatalf("unexpected missing units: %+v", reconcile.MissingUnits)
	}
	if reconcile.MatchedUnits != 3 {
		t.Fatalf("matched units = %d, want 3", reconcile.MatchedUnits)
	}
}

func TestResumeAssessmentProfilePreservesCleanRecords(t *testing.T) {
	// 模拟现网场景：恢复 11 个分片，其中 6 个有缺陷，5 个无缺陷
	plannedUnits := make([]coverage.PlanUnit, 11)
	allAssessments := make([]coverage.AssessmentRecord, 0, 11)

	for i := 0; i < 11; i++ {
		unitID := "hunk-" + string(rune('a'+i))
		plannedUnits[i] = coverage.PlanUnit{
			ID:   unitID,
			Kind: coverage.PlanUnitChangeHunk,
			Path: "file.go",
		}
		// 前 6 个为 defect，后 5 个为 clean
		status := "pass"
		if i < 6 {
			status = "defect"
		}
		// Checkpoint 包含完整的 AssessmentRecords
		allAssessments = append(allAssessments, coverage.AssessmentRecord{
			PrimaryUnitID: unitID,
			Status:        status,
		})
	}

	reconcile := coverage.ReconcilePlanUnits(plannedUnits, allAssessments)
	if len(reconcile.MissingUnits) != 0 {
		t.Fatalf("expected 0 missing units, got: %+v", reconcile.MissingUnits)
	}
	if reconcile.MatchedUnits != 11 {
		t.Fatalf("expected 11 matched units, got: %d", reconcile.MatchedUnits)
	}
}
