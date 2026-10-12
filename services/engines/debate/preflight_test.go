package debate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/plugins"
)

func TestPreflightGatePipelineIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_calc.cpp"
	absPath := filepath.Join(tmpDir, relPath)

	content := `TEST(CalcTest, EmptyStub) {
}

TEST(CalcTest, ValidAdd) {
    EXPECT_EQ(1 + 2, 3);
}
`
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	engine := &DebateEngine{}
	ctx := &engines.EngineContext{
		Ctx:       context.Background(),
		CodesPath: tmpDir,
		Plugins: plugins.PluginsConfig{
			PreflightGate: &plugins.TaskPluginDeclaration{
				ID: "test_case_radar",
			},
		},
	}

	// 1. 测试空桩被直接截断定罪
	emptyUnit := coverage.PlanUnit{
		ID:          "unit-empty",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.EmptyStub",
		StartLine:   1,
		EndLine:     2,
	}

	resEmpty, decided := engine.executePreflightTriage(ctx, emptyUnit)
	if !decided {
		t.Fatal("expected empty stub to be decided by preflight gate, got false")
	}
	if resEmpty.Decision != plugins.DecisionTier0Defect {
		t.Fatalf("expected DecisionTier0Defect, got %s", resEmpty.Decision)
	}
	if resEmpty.Finding == nil || resEmpty.Finding.CalibrationRule != "UT_EMPTY_STUB" {
		t.Fatalf("unexpected finding: %+v", resEmpty.Finding)
	}

	// 2. 测试标准单测被 Fast Pass 放行
	validUnit := coverage.PlanUnit{
		ID:          "unit-valid",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.ValidAdd",
		StartLine:   4,
		EndLine:     6,
	}

	resValid, decidedValid := engine.executePreflightTriage(ctx, validUnit)
	if !decidedValid {
		t.Fatal("expected valid test to be decided by fast pass, got false")
	}
	if resValid.Decision != plugins.DecisionFastPass {
		t.Fatalf("expected DecisionFastPass, got %s", resValid.Decision)
	}

	// 3. 测试 applyPreflightGates 全截断/全放行时的 0 Token 结案行为
	allBundle := chunker.SemanticBundle{
		Name:         "bundle-001",
		PrimaryUnits: []coverage.PlanUnit{emptyUnit, validUnit},
	}

	findings, records, remaining, allDone := engine.applyPreflightGates(ctx, &allBundle)
	if !allDone {
		t.Fatalf("expected all units to be resolved by preflight gate, but remaining %d units", len(remaining))
	}
	if len(findings) != 2 {
		t.Fatalf("expected exactly 2 findings (1 defect, 1 pass), got %d", len(findings))
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 assessment records, got %d", len(records))
	}
	if records[0].Status != "DEFECT" || records[1].Status != "PASS" {
		t.Fatalf("unexpected assessment records: %+v", records)
	}
}

func TestPreflightGateFastPassFindingPropagation(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_calc.cpp"
	absPath := filepath.Join(tmpDir, relPath)

	content := `TEST(CalcTest, ValidAdd) {
    EXPECT_EQ(1 + 2, 3);
}
`
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	engine := &DebateEngine{}
	ctx := &engines.EngineContext{
		Ctx:          context.Background(),
		CodesPath:    tmpDir,
		ReportID:     1001,
		TaskTypeID:   206,
		RepoID:       88,
		Plugins: plugins.PluginsConfig{
			PreflightGate: &plugins.TaskPluginDeclaration{
				ID: "test_case_radar",
			},
		},
	}

	validUnit := coverage.PlanUnit{
		ID:          "unit-valid",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.ValidAdd",
		StartLine:   1,
		EndLine:     3,
	}

	bundle := chunker.SemanticBundle{
		Name:         "bundle-001",
		PrimaryUnits: []coverage.PlanUnit{validUnit},
	}

	findings, records, remaining, allDone := engine.applyPreflightGates(ctx, &bundle)
	if !allDone || len(remaining) != 0 {
		t.Fatalf("expected allDone=true, remaining=%d", len(remaining))
	}
	if len(records) != 1 || records[0].Status != "PASS" {
		t.Fatalf("expected 1 PASS record, got %+v", records)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for pass unit, got %d", len(findings))
	}
	if findings[0].Severity != "合格" || findings[0].AssessmentOutcome != "pass" {
		t.Fatalf("expected pass finding, got %+v", findings[0])
	}
	if findings[0].TaskReportID != 1001 || findings[0].TaskTypeID != 206 || findings[0].RepoID != 88 {
		t.Fatalf("expected metadata injected into finding, got reportID=%d taskType=%d repoID=%d",
			findings[0].TaskReportID, findings[0].TaskTypeID, findings[0].RepoID)
	}
	if findings[0].Title != "测试用例合格：CalcTest.ValidAdd" {
		t.Fatalf("unexpected finding title: %q", findings[0].Title)
	}
}
