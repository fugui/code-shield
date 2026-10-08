package dispatcher

import (
	"context"
	"time"

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
		queueStarted := time.Now()
		tierName := resolveTierName(req, workCtx)
		req.TierName = tierName
		d.RecordDispatchStarted(tierName)
		preferredResourceID := req.ResourceID
		if preferredResourceID == "" && workCtx != nil {
			preferredResourceID = workCtx.ResourceID
		}

		// 1. 申请 LLM 服务器资源（支持模型亲和性与容量加权优先分配）
		res, modelName, err := d.AcquireWithPreference(ctx, backend, req.ModelName, preferredResourceID)
		if err != nil {
			d.RecordDispatchAcquireFailure(tierName, err)
			return invoker.WrapClassifiedError(
				invoker.ErrorClassResourceBusy,
				err,
				"failed to acquire LLM server slot",
			)
		}

		if res != nil {
			queueWait := time.Since(queueStarted)
			if req.Metrics != nil {
				req.Metrics.QueueWaitMs = queueWait.Milliseconds()
			}
			d.RecordDispatchAssigned(tierName, res, queueWait)
			defer d.Release(res, backend)
			// The acquired slot is authoritative: if the logical tier selected a
			// model whose pool filled between selection and acquisition, keep the
			// backend request aligned with the resource that owns this slot.
			if modelName != "" {
				req.ModelName = modelName
			}
			driver := backend
			if res.Driver != "" {
				driver = res.Driver
			}
			obs := invoker.NewCallObservability(req, backend, driver)
			// NewCallObservability receives AIRequest by value. The underlying
			// invoker needs the same diagnostic object to attach stdout/stderr
			// and native stream producers to the lease's ConsoleRing.
			req.Observability = obs
			leaseID := d.RegisterSlotLeaseWithQueue(res, backend, modelName, workCtx, tierName, queueStarted, obs)
			if leaseID != "" {
				defer d.UnregisterSlotLease(leaseID)
			}

			executionStarted := time.Now()
			invokeErr := w.delegate.Invoke(req)
			if req.Metrics != nil {
				req.Metrics.DurationMs = time.Since(executionStarted).Milliseconds()
			}
			obs.FlushConsole()
			obs.Finish(invokeErr)
			d.RecordDispatchCompleted(tierName, res, time.Since(executionStarted), invokeErr)
			d.RecordResourceResult(res, invokeErr)
			d.CompleteSlotLease(leaseID, invokeErr)
			return invokeErr
		}

		d.RecordDispatchPassthrough(tierName)
		executionStarted := time.Now()
		invokeErr := w.delegate.Invoke(req)
		if req.Metrics != nil {
			req.Metrics.DurationMs = time.Since(executionStarted).Milliseconds()
		}
		d.RecordDispatchCompleted(tierName, nil, time.Since(executionStarted), invokeErr)
		return invokeErr
	}

	// 2. 调用底层真正的 AI 驱动
	return w.delegate.Invoke(req)
}
