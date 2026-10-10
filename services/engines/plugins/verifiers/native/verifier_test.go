package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code-shield/services/coverage"
	"code-shield/services/engines/plugins"
	"code-shield/services/invoker"
)

type mockInvoker struct {
	outcome string
	reason  string
	delay   time.Duration
	err     error
}

func (m *mockInvoker) Name() string { return "mock" }
func (m *mockInvoker) Invoke(req invoker.AIRequest) error {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-req.ParentContext.Done():
			return req.ParentContext.Err()
		}
	}
	if m.err != nil {
		return m.err
	}
	resp := TriageLLMResponse{
		Outcome:  m.outcome,
		Reason:   m.reason,
		Severity: "MAJOR",
	}
	raw, _ := json.Marshal(resp)
	return os.WriteFile(req.OutputPath, raw, 0644)
}

func TestNativeSingleRoundVerifierDecisions(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := "test_sample.py"
	absPath := filepath.Join(tmpDir, relPath)
	_ = os.WriteFile(absPath, []byte("def test_cheat():\n    assert True\n"), 0644)

	unit := coverage.PlanUnit{
		ID:          "unit-1",
		Path:        relPath,
		DisplayName: "test_cheat",
		StartLine:   1,
		EndLine:     2,
	}

	verifier := NewNativeSingleRoundVerifier()

	// 1. DEFECT 判定
	verifier.SetInvokerForTest(&mockInvoker{outcome: "DEFECT", reason: "detected assert True"})
	res, err := verifier.Verify(tmpDir, unit, nil, "", 2.0)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if res.Decision != plugins.DecisionTier0Defect {
		t.Fatalf("res.Decision = %s, want %s", res.Decision, plugins.DecisionTier0Defect)
	}
	if res.Finding == nil || res.Finding.CategoryCode != "UTE_TAUTOLOGY_ASSERTION" {
		t.Fatalf("unexpected finding: %+v", res.Finding)
	}

	// 2. PASS 判定
	verifier.SetInvokerForTest(&mockInvoker{outcome: "PASS", reason: "verified by helper"})
	resPass, err := verifier.Verify(tmpDir, unit, nil, "", 2.0)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if resPass.Decision != plugins.DecisionFastPass {
		t.Fatalf("resPass.Decision = %s, want %s", resPass.Decision, plugins.DecisionFastPass)
	}

	// 3. 超时降级放行
	verifier.SetInvokerForTest(&mockInvoker{delay: 100 * time.Millisecond, outcome: "DEFECT"})
	resTimeout, err := verifier.Verify(tmpDir, unit, nil, "", 0.05) // 50ms 超时
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if resTimeout.Decision != plugins.DecisionPass {
		t.Fatalf("resTimeout.Decision = %s, want %s (degraded pass)", resTimeout.Decision, plugins.DecisionPass)
	}

	// 4. 调用失败降级放行
	verifier.SetInvokerForTest(&mockInvoker{err: fmt.Errorf("network error")})
	resErr, err := verifier.Verify(tmpDir, unit, nil, "", 2.0)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if resErr.Decision != plugins.DecisionPass {
		t.Fatalf("resErr.Decision = %s, want %s", resErr.Decision, plugins.DecisionPass)
	}
}

func TestThinVerifierRegistration(t *testing.T) {
	v, err := plugins.GetThinVerifier("native_single_round")
	if err != nil {
		t.Fatalf("GetThinVerifier failed: %v", err)
	}
	if v.ID() != "native_single_round" {
		t.Fatalf("v.ID() = %s, want native_single_round", v.ID())
	}
}
