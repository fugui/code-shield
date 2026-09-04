package dispatcher

import (
	"os"
	"sync/atomic"

	"code-shield/services/invoker"
)

// mockInvoker 用于 dispatcher 单元测试中的模拟驱动
type mockInvoker struct {
	NameStr    string
	InvokedCnt int32
}

func (m *mockInvoker) Name() string {
	return m.NameStr
}

func (m *mockInvoker) Invoke(req invoker.AIRequest) error {
	atomic.AddInt32(&m.InvokedCnt, 1)
	if req.OutputPath != "" {
		if err := os.WriteFile(req.OutputPath, []byte(`{"findings": [], "summary": "mock CLI output"}`), 0644); err != nil {
			return err
		}
	}
	return nil
}
