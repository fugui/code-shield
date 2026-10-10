package testcase

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
)

func writeTempTestFile(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	absPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return absPath
}

func TestCppRadarDecisions(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		code         string
		startLine    int
		endLine      int
		wantDecision plugins.Decision
		wantHintKind string
	}{
		{
			name: "pure empty stub",
			code: `TEST(OrderTest, EmptyStub) {
}`,
			startLine:    1,
			endLine:      2,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "logging only stub",
			code: `TEST(OrderTest, LogOnly) {
    LOG(INFO) << "order processed";
    printf("done\n");
}`,
			startLine:    1,
			endLine:      4,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "tautology literal cheat",
			code: `TEST(OrderTest, Tautology) {
    Order order;
    order.init();
    EXPECT_TRUE(true);
}`,
			startLine:    1,
			endLine:      5,
			wantDecision: plugins.DecisionNeedVerify,
			wantHintKind: "TAUTOLOGY_LITERAL",
		},
		{
			name: "standard valid test",
			code: `TEST(OrderTest, ValidProcess) {
    Order order(100);
    EXPECT_EQ(order.getTotal(), 100);
}`,
			startLine:    1,
			endLine:      4,
			wantDecision: plugins.DecisionFastPass,
		},
		{
			name: "helper assertion call",
			code: `TEST(OrderTest, HelperCall) {
    Order order;
    verifyOrderState(order);
}`,
			startLine:    1,
			endLine:      4,
			wantDecision: plugins.DecisionNeedVerify,
			wantHintKind: "HELPER_ASSERTION_CALL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relPath := filepath.Join("test", "test_order.cpp")
			writeTempTestFile(t, tmpDir, relPath, tt.code)

			unit := coverage.PlanUnit{
				ID:          "unit-cpp-1",
				Kind:        coverage.PlanUnitEntity,
				Path:        relPath,
				DisplayName: "OrderTest.Sample",
				StartLine:   tt.startLine,
				EndLine:     tt.endLine,
			}

			radar := &CppLinterRadar{}
			res := radar.InspectUnit(tmpDir, unit)
			if res.Decision != tt.wantDecision {
				t.Fatalf("Decision = %s, want %s (reason: %s)", res.Decision, tt.wantDecision, res.Reason)
			}
			if tt.wantHintKind != "" {
				found := false
				for _, hint := range res.FactHints {
					if hint.Kind == tt.wantHintKind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected hint kind %q, got hints: %+v", tt.wantHintKind, res.FactHints)
				}
			}
		})
	}
}

func TestPythonRadarDecisions(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		code         string
		startLine    int
		endLine      int
		wantDecision plugins.Decision
		wantHintKind string
	}{
		{
			name: "pass empty stub",
			code: `def test_empty_order():
    pass`,
			startLine:    1,
			endLine:      2,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "print only stub",
			code: `def test_print_only():
    print("running order")`,
			startLine:    1,
			endLine:      2,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "mock typo cheat",
			code: `def test_mock_behavior(mocker):
    service = mocker.MagicMock()
    service.execute()
    service.asser_called()`,
			startLine:    1,
			endLine:      4,
			wantDecision: plugins.DecisionNeedVerify,
			wantHintKind: "MOCK_ASSERTION_TYPO",
		},
		{
			name: "standard assert valid test",
			code: `def test_calculate_total():
    res = calculate_total(10, 20)
    assert res == 30`,
			startLine:    1,
			endLine:      3,
			wantDecision: plugins.DecisionFastPass,
		},
		{
			name: "helper call test",
			code: `def test_user_session():
    session = create_session()
    verify_session_expired(session)`,
			startLine:    1,
			endLine:      3,
			wantDecision: plugins.DecisionNeedVerify,
			wantHintKind: "POTENTIAL_HELPER_VERIFICATION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relPath := filepath.Join("test", "test_order.py")
			writeTempTestFile(t, tmpDir, relPath, tt.code)

			unit := coverage.PlanUnit{
				ID:          "unit-py-1",
				Kind:        coverage.PlanUnitEntity,
				Path:        relPath,
				DisplayName: "test_sample",
				StartLine:   tt.startLine,
				EndLine:     tt.endLine,
			}

			radar := &PythonLinterRadar{}
			res := radar.InspectUnit(tmpDir, unit)
			if res.Decision != tt.wantDecision {
				t.Fatalf("Decision = %s, want %s (reason: %s)", res.Decision, tt.wantDecision, res.Reason)
			}
			if tt.wantHintKind != "" {
				found := false
				for _, hint := range res.FactHints {
					if hint.Kind == tt.wantHintKind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected hint kind %q, got hints: %+v", tt.wantHintKind, res.FactHints)
				}
			}
		})
	}
}

func TestGoRadarDecisions(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		code         string
		startLine    int
		endLine      int
		wantDecision plugins.Decision
		wantHintKind string
	}{
		{
			name: "empty stub",
			code: `func TestEmpty(t *testing.T) {
}`,
			startLine:    1,
			endLine:      2,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "log only stub",
			code: `func TestLogOnly(t *testing.T) {
    fmt.Println("testing something")
}`,
			startLine:    1,
			endLine:      3,
			wantDecision: plugins.DecisionTier0Defect,
		},
		{
			name: "standard assert valid test",
			code: `func TestValid(t *testing.T) {
    got := Add(1, 2)
    assert.Equal(t, 3, got)
}`,
			startLine:    1,
			endLine:      4,
			wantDecision: plugins.DecisionFastPass,
		},
		{
			name: "table driven test",
			code: `func TestTable(t *testing.T) {
    tests := []struct{ a, b, want int }{ {1, 2, 3} }
    for _, tt := range tests {
        t.Run("sub", func(t *testing.T) {
            if tt.a+tt.b != tt.want {
                t.Fatalf("mismatch")
            }
        })
    }
}`,
			startLine:    1,
			endLine:      10,
			wantDecision: plugins.DecisionFastPass,
		},
		{
			name: "tautology assert",
			code: `func TestTautology(t *testing.T) {
    assert.True(t, true)
}`,
			startLine:    1,
			endLine:      3,
			wantDecision: plugins.DecisionNeedVerify,
			wantHintKind: "TAUTOLOGY_LITERAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relPath := filepath.Join("test", "order_test.go")
			writeTempTestFile(t, tmpDir, relPath, tt.code)

			unit := coverage.PlanUnit{
				ID:          "unit-go-1",
				Kind:        coverage.PlanUnitEntity,
				Path:        relPath,
				DisplayName: "TestSample",
				StartLine:   tt.startLine,
				EndLine:     tt.endLine,
			}

			radar := &GoLinterRadar{}
			res := radar.InspectUnit(tmpDir, unit)
			if res.Decision != tt.wantDecision {
				t.Fatalf("Decision = %s, want %s (reason: %s)", res.Decision, tt.wantDecision, res.Reason)
			}
			if tt.wantHintKind != "" {
				found := false
				for _, hint := range res.FactHints {
					if hint.Kind == tt.wantHintKind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected hint kind %q, got hints: %+v", tt.wantHintKind, res.FactHints)
				}
			}
		})
	}
}

func TestTestCaseRadarGateIntegration(t *testing.T) {
	// 验证全局注册中心是否自动挂载该插件
	gate, err := plugins.GetPreflightGate("test_case_radar")
	if err != nil {
		t.Fatalf("GetPreflightGate(test_case_radar) failed: %v", err)
	}
	if gate.ID() != "test_case_radar" {
		t.Fatalf("gate.ID() = %s, want test_case_radar", gate.ID())
	}
}
