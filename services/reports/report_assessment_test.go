package reports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	"code-shield/services/engines/planner"
)

func TestBuildMetaDTOReadsExecutionSnapshot(t *testing.T) {
	root := t.TempDir()
	summaryPath := filepath.Join(root, "diagnostics.json")
	summary := map[string]any{
		"duration_seconds": 1.5,
		"scope_decision": planner.ScopeDecision{
			Decision:         planner.DecisionProceed,
			Reason:           planner.ReasonPrimaryScopeReady,
			ScopeProfile:     "full_review",
			PrimaryUnit:      "file",
			PlanManifestHash: "plan-hash",
		},
		"plan_reconciliation": coverage.PlanReconciliation{
			PlannedUnits: 2,
			MatchedUnits: 1,
			MissingUnits: []string{"unit-missing"},
		},
	}
	summaryBytes, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summaryPath, summaryBytes, 0644); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(root, "report.md")
	if err := os.WriteFile(reportPath, []byte("report"), 0644); err != nil {
		t.Fatal(err)
	}
	report := &models.TaskReport{ID: 1, ReportPath: reportPath, Status: models.StatusDegraded}

	meta := BuildMetaDTO(report)
	if meta.ScopeDecision == nil || meta.ScopeDecision.Decision != planner.DecisionProceed ||
		meta.ScopeDecision.PlanManifestHash != "plan-hash" {
		t.Fatalf("scope decision snapshot not loaded: %+v", meta.ScopeDecision)
	}
	if meta.PlanReconciliation == nil || meta.PlanReconciliation.PlannedUnits != 2 ||
		meta.PlanReconciliation.MatchedUnits != 1 || len(meta.PlanReconciliation.MissingUnits) != 1 {
		t.Fatalf("plan reconciliation snapshot not loaded: %+v", meta.PlanReconciliation)
	}
	if meta.DurationSeconds != 1.5 {
		t.Fatalf("DurationSeconds = %v, want 1.5", meta.DurationSeconds)
	}
}

func TestBuildMetaDTOProjectsChangedCoverage(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	if err := os.WriteFile(reportPath, []byte("report"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := coverage.Coverage{
		PolicyVersion: "v1", CommitVerified: true, WorktreeClean: true, AnalysisComplete: true,
		Files: []coverage.File{
			{Path: "src/a.cpp", Status: coverage.StatusSuccess, DiffTouched: true, HunkRanges: []string{"10-12"}},
			{Path: "src/removed.cpp", Status: coverage.StatusSuccess, DiffTouched: true, Deleted: true, HunkRanges: []string{"1-2"}},
			{Path: "src/context.cpp", Status: coverage.StatusSuccess},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report-1-file-manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatal(err)
	}
	report := &models.TaskReport{ID: 1, ReportPath: reportPath, Status: models.StatusSuccess}

	meta := BuildMetaDTO(report)
	if !meta.CoverageComplete || meta.CoverageDegraded {
		t.Fatalf("unexpected coverage flags: complete=%t degraded=%t", meta.CoverageComplete, meta.CoverageDegraded)
	}
	if len(meta.ChangedCoverageFiles) != 2 {
		t.Fatalf("changed coverage files = %d, want 2", len(meta.ChangedCoverageFiles))
	}
	if meta.ChangedCoverageFiles[0].Path != "src/a.cpp" || meta.ChangedCoverageFiles[1].Path != "src/removed.cpp" {
		t.Fatalf("unexpected changed coverage paths: %+v", meta.ChangedCoverageFiles)
	}
}

func TestLoadFindingsExposesAssessmentArtifacts(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "report.md")
	if err := os.WriteFile(reportPath, []byte("report"), 0644); err != nil {
		t.Fatal(err)
	}
	findingsPath := filepath.Join(root, "findings.json")
	artifact := map[string]any{
		"entity_id": "entity-1",
		"status":    "invalid",
		"issues": []map[string]any{{
			"category":   "断言有效性-空测试",
			"severity":   "严重",
			"detail":     "missing assertion",
			"suggestion": "add assertion",
		}},
	}
	rawFindings := []map[string]any{{
		"id":                       float64(1),
		"file_path":                "src/demo_test.cpp",
		"line_number":              "10-20",
		"title":                    "测试用例需复核",
		"severity":                 "严重",
		"category":                 "断言有效性-空测试",
		"detail":                   "missing assertion",
		"primary_unit_id":          "entity-1",
		"assessment_status":        "invalid",
		"assessment_outcome":       "defect",
		"category_code":            "ASSERTION_EMPTY_TEST",
		"category_source":          "deterministic",
		"category_status":          "VALID",
		"classification_rationale": "controlled rationale",
		"taxonomy_hash":            "sha256:test",
		"assessment_artifact":      artifact,
	}}
	findingsBytes, err := json.Marshal(rawFindings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(findingsPath, findingsBytes, 0644); err != nil {
		t.Fatal(err)
	}
	report := &models.TaskReport{
		ID:         2,
		ReportPath: reportPath,
		TaskType:   models.TaskType{GovernanceMode: models.GovernanceModeEntityAssessment},
		CreatedAt:  time.Now(),
	}

	items, err := loadAllFindingsRaw(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected findings items=%d", len(items))
	}
	item := items[0]
	if item.PrimaryUnitID != "entity-1" || item.AssessmentStatus != "invalid" ||
		item.AssessmentOutcome != "defect" || item.Status != "open" {
		t.Fatalf("unexpected assessment fields: %+v", item)
	}
	if item.CategoryStatus != "VALID" {
		t.Fatalf("assessment category status = %q, want VALID", item.CategoryStatus)
	}
	if item.CategoryCode != "ASSERTION_EMPTY_TEST" || item.CategorySource != "deterministic" ||
		item.ClassificationRationale != "controlled rationale" || item.TaxonomyHash != "sha256:test" {
		t.Fatalf("governed category fields were not exposed: %+v", item)
	}
	if item.AssessmentArtifact == nil || item.AssessmentArtifact["entity_id"] != "entity-1" {
		t.Fatalf("assessment artifact not exposed: %+v", item.AssessmentArtifact)
	}
}

func TestComputeKPIMetricsCountsAssessmentOutcomes(t *testing.T) {
	metrics := computeKPIMetrics([]FindingItemDTO{
		{AssessmentStatus: "REGRESSION"},
		{AssessmentStatus: "REGRESSION"},
		{AssessmentStatus: "EXPOSED_EXISTING"},
		{AssessmentStatus: "SAFE"},
		{AssessmentStatus: "NEEDS_HUMAN"},
	}, false)
	if metrics.AssessmentStats["defect"] != 3 ||
		metrics.AssessmentStats["pass"] != 1 ||
		metrics.AssessmentStats["needs_human"] != 1 {
		t.Fatalf("unexpected assessment stats: %+v", metrics.AssessmentStats)
	}
}

func TestGetReportFindingsEntityFilterMappings(t *testing.T) {
	items := []FindingItemDTO{
		{Severity: SeverityPass, Status: "pass", AssessmentOutcome: string(assessment.OutcomePass)},
		{Severity: SeverityCritical, Status: "open", AssessmentOutcome: string(assessment.OutcomeDefect)},
		{Severity: SeveritySuggestion, Status: "open", AssessmentOutcome: string(assessment.OutcomeDefect)},
		{Severity: SeverityMajor, Status: "analyzing", AssessmentOutcome: string(assessment.OutcomeNeedsHuman)},
	}
	metrics := computeKPIMetrics(items, true)
	if metrics.TotalFindings != 4 || metrics.PassCount != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}

	assertMatches := func(query FindingsQuery, expected int) {
		t.Helper()
		matched := 0
		for _, item := range items {
			if matchesReportQuery(item, query, true) {
				matched++
			}
		}
		if matched != expected {
			t.Fatalf("query %+v matched %d items, want %d", query, matched, expected)
		}
	}

	assertMatches(FindingsQuery{Severity: "pass"}, 1)
	assertMatches(FindingsQuery{Severity: "fatal,critical,major,minor,suggestion"}, 3)
	assertMatches(FindingsQuery{Status: "pass"}, 1)
	assertMatches(FindingsQuery{Status: "fail"}, 2)
}
