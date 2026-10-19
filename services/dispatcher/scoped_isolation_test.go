package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

// TestDispatcher_ScopedIsolation_NeverBorrowsUnreferencedPool 验证核心隔离准则：
// 当被引用的算力池打满时，调度器绝不越界借用未引用的算力池，必须阻塞等待直至已授权节点释放。
func TestDispatcher_ScopedIsolation_NeverBorrowsUnreferencedPool(t *testing.T) {
	poolA := &ModelResource{
		Index:      0,
		ID:         "pool-a",
		Driver:     "native",
		Native:     "model-a",
		Concurrent: 1,
		Active:     0,
	}
	poolB := &ModelResource{
		Index:      1,
		ID:         "pool-b",
		Driver:     "native",
		Native:     "model-b",
		Concurrent: 2,
		Active:     0,
	}

	d := &ModelDispatcher{
		enabled:       true,
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		resources:     []*ModelResource{poolA, poolB},
	}
	d.cond = sync.NewCond(&d.mu)
	defer close(d.stopHeartbeat)

	allowedIDs := []string{"pool-a"}

	// 1. 第一个请求占用 pool-a 唯一槽位
	ctx1 := context.Background()
	res1, _, err1 := d.AcquireScoped(ctx1, "native", "", "", allowedIDs)
	if err1 != nil || res1 == nil || res1.ID != "pool-a" {
		t.Fatalf("expected acquire pool-a, got res=%v, err=%v", res1, err1)
	}
	if poolA.Active != 1 {
		t.Fatalf("expected pool-a active=1, got %d", poolA.Active)
	}

	// 2. 第二个请求发起，此时 pool-a 已满，而 pool-b 有 2 个空闲槽位
	acquiredCh := make(chan *ModelResource, 1)
	errCh := make(chan error, 1)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	go func() {
		res, _, err := d.AcquireScoped(ctx2, "native", "", "", allowedIDs)
		if err != nil {
			errCh <- err
			return
		}
		acquiredCh <- res
	}()

	// 等待 80ms，确保第二个请求已进入调度循环并阻塞在条件变量上
	time.Sleep(80 * time.Millisecond)

	// 验证：pool-b 绝对未被越界借用！Active 依然为 0
	d.mu.Lock()
	if poolB.Active != 0 {
		d.mu.Unlock()
		t.Fatalf("VIOLATION: unreferenced pool-b was borrowed! Active=%d, want 0", poolB.Active)
	}
	d.mu.Unlock()

	select {
	case res := <-acquiredCh:
		t.Fatalf("expected request 2 to block waiting for pool-a, but it acquired immediately: %s", res.ID)
	default:
		// 正常阻塞在已授权池 pool-a 上
	}

	// 3. 第一个请求释放 pool-a
	d.Release(res1, "native")

	// 4. 第二个请求必须被唤醒并在 pool-a 上获得槽位
	select {
	case res2 := <-acquiredCh:
		if res2.ID != "pool-a" {
			t.Fatalf("expected request 2 to acquire pool-a upon release, got %s", res2.ID)
		}
		defer d.Release(res2, "native")
	case err := <-errCh:
		t.Fatalf("unexpected error for request 2: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatalf("request 2 timed out waiting for release of pool-a")
	}

	// 验证全过程中未引用的 pool-b 从始至终零污染
	if poolB.Active != 0 {
		t.Fatalf("pool-b active was touched: %d", poolB.Active)
	}
}

// TestDispatcher_ScopedIsolation_IntraPoolBalancing 验证多选算力池集内负载均衡：
// 流量仅在已授权的算力池之间打散，未引用的算力池不接收任何流量。
func TestDispatcher_ScopedIsolation_IntraPoolBalancing(t *testing.T) {
	poolA := &ModelResource{Index: 0, ID: "pool-a", Driver: "native", Native: "model-a", Concurrent: 1}
	poolB := &ModelResource{Index: 1, ID: "pool-b", Driver: "native", Native: "model-b", Concurrent: 1}
	poolC := &ModelResource{Index: 2, ID: "pool-c", Driver: "native", Native: "model-c", Concurrent: 5}

	d := &ModelDispatcher{
		enabled:       true,
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		resources:     []*ModelResource{poolA, poolB, poolC},
	}
	d.cond = sync.NewCond(&d.mu)
	defer close(d.stopHeartbeat)

	allowedIDs := []string{"pool-a", "pool-b"}

	// 请求 1 分配 pool-a
	res1, _, err := d.AcquireScoped(context.Background(), "native", "", "pool-a", allowedIDs)
	if err != nil || res1.ID != "pool-a" {
		t.Fatalf("res1 expected pool-a, got %v, err=%v", res1, err)
	}
	defer d.Release(res1, "native")

	// 请求 2 偏好 pool-a，但 pool-a 已满。因为 pool-b 在 allowedIDs 中，应在集内切换到 pool-b
	res2, _, err := d.AcquireScoped(context.Background(), "native", "", "pool-a", allowedIDs)
	if err != nil || res2.ID != "pool-b" {
		t.Fatalf("res2 expected intra-pool balancing to pool-b, got %v, err=%v", res2, err)
	}
	defer d.Release(res2, "native")

	// 请求 3：pool-a 和 pool-b 均已满，虽然未引用的 pool-c 有 5 个空闲，但绝对不能使用 pool-c
	ctx3, cancel3 := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel3()

	_, _, err3 := d.AcquireScoped(ctx3, "native", "", "", allowedIDs)
	if err3 == nil {
		t.Fatalf("expected timeout error for request 3, but got slot!")
	}

	if poolC.Active != 0 {
		t.Fatalf("unreferenced pool-c active must be 0, got %d", poolC.Active)
	}
}

// TestDispatcher_ScopedIsolation_RejectsUnsupportedBackend 验证非法配置快速报错：
// 当阶段配置的算力池不支持当前阶段的 backend 时，立即返回错误而不卡死。
func TestDispatcher_ScopedIsolation_RejectsUnsupportedBackend(t *testing.T) {
	poolA := &ModelResource{Index: 0, ID: "thick-pool", Driver: "opencode", OpenCode: "glm-5", Concurrent: 2}

	d := &ModelDispatcher{
		enabled:       true,
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		resources:     []*ModelResource{poolA},
	}
	d.cond = sync.NewCond(&d.mu)
	defer close(d.stopHeartbeat)

	// 请求 native 驱动，但授权的 thick-pool 仅支持 opencode
	_, _, err := d.AcquireScoped(context.Background(), "native", "", "", []string{"thick-pool"})
	if err == nil {
		t.Fatalf("expected error for unsupported backend on allowed pool, got nil")
	}
}

// TestDispatchingInvoker_TierIsolationEndToEnd 验证端到端流水线阶段绑定防护：
// 当通过 DispatchingInvoker 执行指定 Tier 时，只使用该 Tier 绑定的池。
func TestDispatchingInvoker_TierIsolationEndToEnd(t *testing.T) {
	challengerPool := &ModelResource{Index: 0, ID: "challenger-pool", Driver: "native", Native: "qwen-coder", Concurrent: 1}
	judgePool := &ModelResource{Index: 1, ID: "judge-pool", Driver: "native", Native: "deepseek-r1", Concurrent: 2}

	d := &ModelDispatcher{
		enabled:       true,
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		resources:     []*ModelResource{challengerPool, judgePool},
		activeLeases:  make(map[string]*LLMSlotLease),
	}
	d.cond = sync.NewCond(&d.mu)
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 配置 Tier 2 Challenger 仅允许 challenger-pool
	models.AppConfig.Scanner.Debate.Tiers.Tier2Challenger = models.TierBindingConfig{
		Resources: []string{"challenger-pool"},
	}

	mock := &mockInvoker{NameStr: "native"}
	wrapped := NewDispatchingInvoker(mock, d)

	// 先占满 challengerPool
	challengerPool.Active = 1

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	req := invoker.AIRequest{
		ParentContext: ctx,
		TierName:      "tier2_challenger",
		ModelName:     "qwen-coder",
	}

	err := wrapped.Invoke(req)
	if err == nil {
		t.Fatalf("expected invocation to fail due to challenger-pool full, but succeeded!")
	}

	// 确认 judge-pool 绝未被越界借用
	if judgePool.Active != 0 {
		t.Fatalf("judge-pool was violated by challenger task! Active=%d, want 0", judgePool.Active)
	}
}
