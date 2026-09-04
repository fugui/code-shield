package dispatcher

import (
	"context"
	"fmt"

	"code-shield/services/invoker"
)

// DispatchingInvoker 是 AIInvoker 的代理，自动在调用前后向 ModelDispatcher 申请/释放并发槽位并登记租约
type DispatchingInvoker struct {
	delegate   invoker.AIInvoker
	dispatcher *ModelDispatcher
}

// WrapInvoker 使用全局调度器包装原始 AIInvoker。若全局调度器未启用或未初始化，直接返回原始 invoker
func WrapInvoker(inv invoker.AIInvoker) invoker.AIInvoker {
	if inv == nil {
		return nil
	}
	d := GlobalDispatcher
	if d == nil || !d.enabled {
		return inv
	}
	return &DispatchingInvoker{
		delegate:   inv,
		dispatcher: d,
	}
}

// NewDispatchingInvoker 使用指定调度器包装 AIInvoker
func NewDispatchingInvoker(inv invoker.AIInvoker, d *ModelDispatcher) invoker.AIInvoker {
	if inv == nil {
		return nil
	}
	if d == nil || !d.enabled {
		return inv
	}
	return &DispatchingInvoker{
		delegate:   inv,
		dispatcher: d,
	}
}

func (w *DispatchingInvoker) Name() string {
	return w.delegate.Name()
}

func (w *DispatchingInvoker) Invoke(req invoker.AIRequest) error {
	backend := w.delegate.Name()
	ctx := req.ParentContext
	if ctx == nil {
		ctx = context.Background()
	}

	workCtx := req.WorkContext
	if workCtx == nil {
		workCtx = invoker.LLMWorkContextFromContext(ctx)
	}

	d := w.dispatcher
	if d == nil {
		d = GlobalDispatcher
	}

	if d != nil && d.enabled {
		// 1. 申请 LLM 服务器资源（支持模型亲和性与容量加权优先分配）
		res, modelName, err := d.AcquireWithPreference(ctx, backend, req.ModelName)
		if err != nil {
			return fmt.Errorf("failed to acquire LLM server slot: %w", err)
		}

		if res != nil {
			defer d.Release(res, backend)
			if req.ModelName == "" && modelName != "" {
				req.ModelName = modelName
			}
			leaseID := d.RegisterSlotLease(res, backend, modelName, workCtx)
			if leaseID != "" {
				defer d.UnregisterSlotLease(leaseID)
			}
		}
	}

	// 2. 调用底层真正的 AI 驱动
	return w.delegate.Invoke(req)
}
