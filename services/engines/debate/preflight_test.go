package debate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/plugins"
	"code-shield/services/invoker"
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
		Ctx:        context.Background(),
		CodesPath:  tmpDir,
		ReportID:   1001,
		TaskTypeID: 206,
		RepoID:     88,
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

type mockConcurrentVerifier struct {
	calledCount int
}

func (m *mockConcurrentVerifier) ID() string {
	return "mock_concurrent_verifier"
}

func (m *mockConcurrentVerifier) Verify(codesPath string, unit coverage.PlanUnit, hints []plugins.StructuralFactHint, promptTemplate string, timeoutSec float64) (plugins.RadarResult, error) {
	return plugins.RadarResult{
		Decision: plugins.DecisionTier0Defect,
		Finding: &models.AnalysisFinding{
			PrimaryUnitID: unit.ID,
			Severity:      "严重",
			Category:      "断言有效性-预期结果不完整",
			CategoryCode:  "UTE_INCOMPLETE_EXPECTATION",
			Title:         "复核确认为缺陷",
		},
	}, nil
}

func TestPreflightGateConcurrentThinVerifier(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_concurrent.go"
	absPath := filepath.Join(tmpDir, relPath)

	// 编写包含 5 个仅带弱断言的 Go 测试用例
	content := `package test
import "testing"
import "github.com/stretchr/testify/assert"

func TestUnit1(t *testing.T) {
    x := 1
    y := 2
    z := 3
    w := x + y + z
    assert.NotNil(t, w)
}
func TestUnit2(t *testing.T) {
    x := 1
    y := 2
    z := 3
    w := x + y + z
    assert.NotNil(t, w)
}
func TestUnit3(t *testing.T) {
    x := 1
    y := 2
    z := 3
    w := x + y + z
    assert.NotNil(t, w)
}
func TestUnit4(t *testing.T) {
    x := 1
    y := 2
    z := 3
    w := x + y + z
    assert.NotNil(t, w)
}
func TestUnit5(t *testing.T) {
    x := 1
    y := 2
    z := 3
    w := x + y + z
    assert.NotNil(t, w)
}
`
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mockVerifier := &mockConcurrentVerifier{}
	_ = plugins.RegisterThinVerifier(mockVerifier)

	engine := &DebateEngine{}
	ctx := &engines.EngineContext{
		Ctx:        context.Background(),
		CodesPath:  tmpDir,
		ReportID:   2001,
		TaskTypeID: 206,
		RepoID:     99,
		Plugins: plugins.PluginsConfig{
			PreflightGate: &plugins.TaskPluginDeclaration{
				ID: "test_case_radar",
			},
			ThinVerifier: &plugins.TaskPluginDeclaration{
				ID: "mock_concurrent_verifier",
			},
		},
	}

	var units []coverage.PlanUnit
	lineRanges := [][2]int{{4, 11}, {12, 18}, {19, 25}, {26, 32}, {33, 39}}
	for i, r := range lineRanges {
		units = append(units, coverage.PlanUnit{
			ID:          filepath.Join("unit", string(rune('1'+i))),
			Kind:        coverage.PlanUnitEntity,
			Path:        relPath,
			DisplayName: "TestUnit",
			StartLine:   r[0],
			EndLine:     r[1],
		})
	}

	bundle := chunker.SemanticBundle{
		Name:         "bundle-concurrent-001",
		PrimaryUnits: units,
	}

	findings, records, remaining, allDone := engine.applyPreflightGates(ctx, &bundle)
	if !allDone || len(remaining) != 0 {
		t.Fatalf("expected allDone=true, remaining=%d", len(remaining))
	}
	if len(findings) != 5 {
		t.Fatalf("expected 5 defect findings, got %d", len(findings))
	}
	if len(records) != 5 {
		t.Fatalf("expected 5 defect records, got %d", len(records))
	}
	for _, rec := range records {
		if rec.Status != "DEFECT" {
			t.Fatalf("expected record status DEFECT, got %s", rec.Status)
		}
	}
}

func TestRunSpecializedAssessmentPreservesPreflightOnLLMFailure(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_mixed.cpp"
	absPath := filepath.Join(tmpDir, relPath)

	// 包含 1 个空测试（会被 preflight 拦截）和 1 个具有孤立弱断言的长用例（进入 LLM 评估）
	content := `TEST(CalcTest, EmptyStub) {
}

TEST(CalcTest, ComplexNeedsLLM) {
    auto res = ComputeSomething(1, 2);
    StepTwo(res);
    StepThree(res);
    EXPECT_TRUE(res.IsValid());
}
`
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	primary := registerRecoveryInvoker(t, "preflight-llm-fail", func(call int) error {
		return invoker.NewClassifiedError(invoker.ErrorClassTimeout, "llm timed out")
	})
	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       1,
		MaxAttemptsPerResource: 1,
		SplitOn:                []string{"idle_timeout"}, // 不触发 split，直接走失败路径
	})

	engine := &DebateEngine{}
	ctx := &engines.EngineContext{
		Ctx:               context.Background(),
		CodesPath:         tmpDir,
		EngineMode:        "debate_full",
		AssessmentProfile: "entityreview",
		AllowedCategories: []string{"无问题", "断言有效性-空测试"},
		Plugins: plugins.PluginsConfig{
			PreflightGate: &plugins.TaskPluginDeclaration{
				ID: "test_case_radar",
			},
		},
	}

	emptyUnit := coverage.PlanUnit{
		ID:          "unit-empty",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.EmptyStub",
		StartLine:   1,
		EndLine:     2,
	}
	complexUnit := coverage.PlanUnit{
		ID:          "unit-complex",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.ComplexNeedsLLM",
		StartLine:   4,
		EndLine:     9,
	}

	bundle := chunker.SemanticBundle{
		Name:         "bundle-preflight-fail",
		PrimaryFiles: []string{relPath},
		AllFiles:     []string{relPath},
		PrimaryUnits: []coverage.PlanUnit{emptyUnit, complexUnit},
	}

	findings, records, _, err := engine.runSpecializedAssessment(ctx, bundle, filepath.Join(tmpDir, "assessment.json"))
	if err == nil {
		t.Fatal("expected LLM error, got nil")
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 preflight finding preserved, got %d", len(findings))
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 preflight record preserved, got %d", len(records))
	}
	if findings[0].CalibrationRule != "UT_EMPTY_STUB" {
		t.Fatalf("expected UT_EMPTY_STUB finding, got %+v", findings[0])
	}
	if records[0].PrimaryUnitID != "unit-empty" || records[0].Status != "DEFECT" {
		t.Fatalf("unexpected record: %+v", records[0])
	}

	// 进一步验证 ProcessBundle 返回的局部结果
	pbFindings, _, pbRecords, _, _, pbErr := engine.ProcessBundle(ctx, bundle, 0, tmpDir, nil)
	if pbErr == nil {
		t.Fatal("expected ProcessBundle to return error, got nil")
	}
	if len(pbFindings) != 1 || len(pbRecords) != 1 {
		t.Fatalf("expected ProcessBundle to preserve 1 finding and 1 record, got %d findings and %d records",
			len(pbFindings), len(pbRecords))
	}
}

func TestRunSpecializedAssessmentPreservesPartialLLMAndPreflightFindingsOnSubBundleFailure(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_partial_split.cpp"
	absPath := filepath.Join(tmpDir, relPath)

	content := `TEST(CalcTest, EmptyStub) {
}

TEST(CalcTest, ComplexOne) {
    auto res = ComputeSomething(1, 2);
    StepTwo(res);
    StepThree(res);
    EXPECT_TRUE(res.IsValid());
}

TEST(CalcTest, ComplexTwo) {
    auto res = ComputeSomething(3, 4);
    StepTwo(res);
    StepThree(res);
    EXPECT_TRUE(res.IsValid());
}
`
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 模拟 invoker 行为：
	// Call 1: 父分片调用 -> 超时触发 split
	// Call 2: 子分片 1 -> 成功返回合格 assessment 结果
	// Call 3: 子分片 2 -> 超时失败
	primary := registerRecoveryInvoker(t, "preflight-partial-split-llm", func(call int) error {
		if call == 1 || call == 3 {
			return invoker.NewClassifiedError(invoker.ErrorClassTimeout, "timed out")
		}
		return nil
	})
	primary.payload = func(call int) string {
		if call == 2 {
			return `{"schema":"code-shield.unit-assessments.v2","assessments":[
				{"unit_ref":"u001","outcome":"pass","summary":"safe and valid"}
			]}`
		}
		return ""
	}

	configureAssessmentRecovery(t, primary.name, models.TierRecoveryConfig{
		MaxTotalAttempts:       1,
		MaxAttemptsPerResource: 1,
		SplitOn:                []string{"timeout"},
	})

	engine := &DebateEngine{}
	ctx := &engines.EngineContext{
		Ctx:               context.Background(),
		CodesPath:         tmpDir,
		EngineMode:        "debate_full",
		AssessmentProfile: "entityreview",
		AllowedCategories: []string{"无问题", "断言有效性-空测试"},
		Plugins: plugins.PluginsConfig{
			PreflightGate: &plugins.TaskPluginDeclaration{
				ID: "test_case_radar",
			},
		},
	}

	emptyUnit := coverage.PlanUnit{
		ID:          "unit-empty",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.EmptyStub",
		StartLine:   1,
		EndLine:     2,
	}
	complex1 := coverage.PlanUnit{
		ID:          "unit-c1",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.ComplexOne",
		StartLine:   4,
		EndLine:     9,
	}
	complex2 := coverage.PlanUnit{
		ID:          "unit-c2",
		Kind:        coverage.PlanUnitEntity,
		Path:        relPath,
		DisplayName: "CalcTest.ComplexTwo",
		StartLine:   11,
		EndLine:     16,
	}

	bundle := chunker.SemanticBundle{
		Name:         "bundle-partial-split",
		PrimaryFiles: []string{relPath},
		AllFiles:     []string{relPath},
		PrimaryUnits: []coverage.PlanUnit{emptyUnit, complex1, complex2},
	}

	findings, records, _, err := engine.runSpecializedAssessment(ctx, bundle, filepath.Join(tmpDir, "assessment.json"))
	if err == nil {
		t.Fatal("expected error due to sub-bundle failure, got nil")
	}
	// 预期：保留 2 个 findings (1 个 preflight 缺陷 EmptyStub + 1 个子分片 1 的 LLM finding)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (1 preflight + 1 LLM) preserved, got %d: %+v", len(findings), findings)
	}
	if findings[0].CalibrationRule != "UT_EMPTY_STUB" || findings[1].PrimaryUnitID != "unit-c1" {
		t.Fatalf("unexpected findings preserved: %+v", findings)
	}
	// 预期：保留 2 个 records（1 个来自 Preflight 的 DEFECT，1 个来自子分片 1 的成功 LLM record）
	if len(records) != 2 {
		t.Fatalf("expected 2 records (1 preflight + 1 LLM) preserved, got %d: %+v", len(records), records)
	}
	if records[0].PrimaryUnitID != "unit-empty" || records[1].PrimaryUnitID != "unit-c1" {
		t.Fatalf("unexpected records preserved: %+v", records)
	}
}

