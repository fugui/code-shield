package services

import (
	"os"
	"sync/atomic"
)

// MockInvoker 用于单测中的通用模拟 AI 驱动
type MockInvoker struct {
	NameStr    string
	InvokedCnt int32
}

func (m *MockInvoker) Name() string {
	return m.NameStr
}

func (m *MockInvoker) Invoke(req AIRequest) error {
	atomic.AddInt32(&m.InvokedCnt, 1)
	if req.OutputPath != "" {
		if err := os.WriteFile(req.OutputPath, []byte(`{"findings": [], "summary": "mock CLI output"}`), 0644); err != nil {
			return err
		}
	}
	return nil
}
