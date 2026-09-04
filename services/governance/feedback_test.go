package governance

import (
	"os"
	"path/filepath"
	"testing"

	"code-shield/services/invoker"
)

type mockFeedbackInvoker struct {
	responseJSON string
}

func (m *mockFeedbackInvoker) Name() string { return "mock-feedback" }

func (m *mockFeedbackInvoker) Invoke(req invoker.AIRequest) error {
	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(req.OutputPath, []byte(m.responseJSON), 0644)
	}
	return nil
}

func TestExtractFeedbackRule(t *testing.T) {
	mockResp := `{"scope_type": "FILE", "pattern": "src/crypto/aes.go", "rule_action": "IGNORE", "reason": "已由外部硬件安全模块保护"}`
	inv := &mockFeedbackInvoker{responseJSON: mockResp}

	rule, err := ExtractFeedbackRule(inv, "src/crypto/aes.go", "AESKey = nil", "Hardcoded Key", "采用外部 HSM 密钥管理")
	if err != nil {
		t.Fatalf("ExtractFeedbackRule failed: %v", err)
	}
	if rule == nil {
		t.Fatal("expected non-nil rule")
	}
	if rule.Pattern != "src/crypto/aes.go" || rule.Reason != "已由外部硬件安全模块保护" {
		t.Fatalf("unexpected extracted rule: %+v", rule)
	}
}
