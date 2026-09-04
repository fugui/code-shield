package governance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"code-shield/models"
	"code-shield/services/invoker"
)

func TestSanitizeFindingTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "未命名缺陷"},
		{"   ", "未命名缺陷"},
		{"正常标题", "正常标题"},
		{"包含\n换行\r\n的标题", "包含 换行 的标题"},
		{strings.Repeat("测", 600), strings.Repeat("测", 497) + "..."},
	}

	for _, tt := range tests {
		got := SanitizeFindingTitle(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeFindingTitle(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseLineInterval(t *testing.T) {
	s, e := ParseLineInterval("")
	if s != 0 || e != 0 {
		t.Errorf("expected 0, 0, got %d, %d", s, e)
	}

	s, e = ParseLineInterval("L42")
	if s != 42 || e != 42 {
		t.Errorf("expected 42, 42, got %d, %d", s, e)
	}

	s, e = ParseLineInterval("10-25")
	if s != 10 || e != 25 {
		t.Errorf("expected 10, 25, got %d, %d", s, e)
	}

	s, e = ParseLineInterval("15, 8, 30")
	if s != 8 || e != 30 {
		t.Errorf("expected 8, 30, got %d, %d", s, e)
	}
}

func TestCalculateLineSimilarity(t *testing.T) {
	// 完全相同
	sim := CalculateLineSimilarity("10-20", "10-20")
	if sim < 0.99 {
		t.Errorf("expected 1.0, got %f", sim)
	}

	// 邻近行 (例如 20 和 25，距离 5)
	simClose := CalculateLineSimilarity("20", "25")
	if simClose <= 0 || simClose > 1.0 {
		t.Errorf("expected close similarity in (0, 1], got %f", simClose)
	}

	// 距离远 (>15 行)
	simFar := CalculateLineSimilarity("10", "100")
	if simFar != 0.0 {
		t.Errorf("expected 0.0 for far lines, got %f", simFar)
	}
}

func TestNormalizeCodeAndHash(t *testing.T) {
	code1 := "func foo() {\n    // comment\n    return 42;\n}"
	code2 := "func foo() {/* block */ return 42;}"

	hash1 := ComputeCodeHash(code1)
	hash2 := ComputeCodeHash(code2)

	if hash1 == "" || hash2 == "" {
		t.Fatalf("expected non-empty hashes")
	}
	if hash1 != hash2 {
		t.Errorf("expected equal hash after normalization: %s vs %s", hash1, hash2)
	}
}

func TestCalculateStringSimilarity(t *testing.T) {
	if sim := CalculateStringSimilarity("", ""); sim != 1.0 {
		t.Errorf("expected 1.0 for both empty, got %f", sim)
	}
	if sim := CalculateStringSimilarity("hello", ""); sim != 0.0 {
		t.Errorf("expected 0.0 for one empty, got %f", sim)
	}
	if sim := CalculateStringSimilarity("空指针解引用", "空指针解引用"); sim != 1.0 {
		t.Errorf("expected 1.0 for identical string, got %f", sim)
	}
	sim := CalculateStringSimilarity("空指针解引用风险", "空指针解引用")
	if sim <= 0.5 || sim >= 1.0 {
		t.Errorf("expected high similarity, got %f", sim)
	}
}

// mockCampaignInvoker 模拟 LLM 语义判断
type mockCampaignInvoker struct {
	isSame bool
}

func (m *mockCampaignInvoker) Name() string {
	return "mock"
}

func (m *mockCampaignInvoker) Invoke(req invoker.AIRequest) error {
	jsonContent := fmt.Sprintf(`{"is_same": %t}`, m.isSame)
	return os.WriteFile(req.OutputPath, []byte(jsonContent), 0644)
}

func TestAskLLMIfSameFinding(t *testing.T) {
	mockInv := &mockCampaignInvoker{isSame: true}
	ctx := &CampaignContext{
		Ctx:     context.Background(),
		Invoker: mockInv,
		Report:  models.TaskReport{ID: 101},
	}

	isSame := AskLLMIfSameFinding(ctx, "a.go", "10", "Title1", "Detail1", "snippet1", "a.go", "12", "Title2", "Detail2", "snippet2")
	if !isSame {
		t.Errorf("expected true from mock LLM invoker")
	}

	mockInv.isSame = false
	isSame = AskLLMIfSameFinding(ctx, "a.go", "10", "Title1", "Detail1", "snippet1", "a.go", "12", "Title2", "Detail2", "snippet2")
	if isSame {
		t.Errorf("expected false from mock LLM invoker")
	}

	// nil invoker
	ctx.Invoker = nil
	if AskLLMIfSameFinding(ctx, "", "", "", "", "", "", "", "", "", "") {
		t.Errorf("expected false when invoker is nil")
	}
}
