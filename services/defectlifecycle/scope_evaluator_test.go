package defectlifecycle

import (
	"testing"

	"code-shield/models"
)

func TestMemoryScopeEvaluator(t *testing.T) {
	entries := []models.ScanScopeEntry{
		{
			NormPath:    "src/healthy.cpp",
			Outcome:     ScopeScanned,
			DiffTouched: false,
		},
		{
			NormPath:    "src/modified.cpp",
			Outcome:     ScopeScanned,
			DiffTouched: true,
		},
		{
			NormPath:    "src/failed.cpp",
			Outcome:     ScopeFailed,
			FailReason:  "parser crashed",
			DiffTouched: true,
		},
	}

	evaluator := NewMemoryScopeEvaluator(entries)

	// 1. 健康未修改文件：已覆盖、未变更、未失败
	if !evaluator.IsCovered("src/healthy.cpp") {
		t.Errorf("expected src/healthy.cpp to be covered")
	}
	if evaluator.IsChanged("src/healthy.cpp") {
		t.Errorf("expected src/healthy.cpp not to be changed")
	}
	if failed, _ := evaluator.IsFailed("src/healthy.cpp"); failed {
		t.Errorf("expected src/healthy.cpp not to be failed")
	}

	// 2. 变更文件：已覆盖、已变更、未失败
	if !evaluator.IsCovered("src/modified.cpp") {
		t.Errorf("expected src/modified.cpp to be covered")
	}
	if !evaluator.IsChanged("src/modified.cpp") {
		t.Errorf("expected src/modified.cpp to be changed")
	}
	if failed, _ := evaluator.IsFailed("src/modified.cpp"); failed {
		t.Errorf("expected src/modified.cpp not to be failed")
	}

	// 3. 失败文件：未覆盖（防止误判修复）、已失败
	if evaluator.IsCovered("src/failed.cpp") {
		t.Errorf("expected src/failed.cpp NOT to be covered due to failure")
	}
	if failed, reason := evaluator.IsFailed("src/failed.cpp"); !failed || reason != "parser crashed" {
		t.Errorf("expected src/failed.cpp to be failed with parser crashed, got failed=%v, reason=%q", failed, reason)
	}

	// 4. 计划外文件：未覆盖、未变更、未失败
	if evaluator.IsCovered("src/unplanned.cpp") {
		t.Errorf("expected src/unplanned.cpp NOT to be covered")
	}
	if evaluator.IsChanged("src/unplanned.cpp") {
		t.Errorf("expected src/unplanned.cpp NOT to be changed")
	}

	// 5. 空指针保护
	var nilEval *MemoryScopeEvaluator
	if nilEval.IsCovered("src/any.cpp") {
		t.Errorf("nil evaluator should not report covered")
	}
	if nilEval.IsChanged("src/any.cpp") {
		t.Errorf("nil evaluator should not report changed")
	}
	if failed, _ := nilEval.IsFailed("src/any.cpp"); failed {
		t.Errorf("nil evaluator should not report failed")
	}
}
