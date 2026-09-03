package services

import (
	"code-shield/models"
	"context"
	"sync"
	"testing"
	"time"
)

// TestDispatcher_HotReload_InFlightRelease 验证调用在飞行期间发生热重载时，Release 能否正确归还当前生效资源槽位
func TestDispatcher_HotReload_InFlightRelease(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{
			Index:      0,
			ID:         "opencode-deepseek",
			Driver:     "opencode",
			OpenCode:   "deepseek-chat",
			Concurrent: 2,
			Active:     0,
		},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. 在途调用申请槽位
	res, model, err := d.Acquire(ctx, "opencode")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if model != "deepseek-chat" {
		t.Fatalf("unexpected model: %s", model)
	}
	if res.Active != 1 {
		t.Fatalf("expected res.Active == 1, got %d", res.Active)
	}

	// 2. 在调用执行期间发生热重载（修改配置并发为 10）
	newCfg := models.LLMConfig{
		Resources: []models.ComputeResourceConfig{
			{
				ID:         "opencode-deepseek",
				Driver:     "opencode",
				Model:      "deepseek-chat",
				Concurrent: 10,
			},
		},
	}
	d.ReloadResources(newCfg)

	// 验证热重载后当前资源状态
	if len(d.resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(d.resources))
	}
	curRes := d.resources[0]
	if curRes.Concurrent != 10 {
		t.Fatalf("expected concurrent updated to 10, got %d", curRes.Concurrent)
	}
	if curRes.Active != 1 {
		t.Fatalf("expected curRes.Active to maintain 1, got %d", curRes.Active)
	}

	// 3. 在途调用结束并释放原指针
	d.Release(res, "opencode")

	// 验证当前资源池中 Active 计数必须精准回落到 0，绝不产生孤儿泄漏
	if curRes.Active != 0 {
		t.Fatalf("expected curRes.Active == 0 after Release, got %d (orphan leak detected!)", curRes.Active)
	}
}

// TestDispatcher_HotReload_StalePointerFallback 验证即使指针地址不同（如旧实例），Release 也可通过 Key 扣减当前生效资源的 Active
func TestDispatcher_HotReload_StalePointerFallback(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)

	// 当前生效的新对象
	currentRes := &ModelResource{
		Index:      0,
		ID:         "native-cluster",
		Driver:     "native",
		Native:     "deepseek-v4",
		Concurrent: 5,
		Active:     1, // 继承自上代的在途活跃计数
	}
	d.resources = []*ModelResource{currentRes}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 模拟旧代指针（地址与 currentRes 不同，但 Key 相同）
	staleRes := &ModelResource{
		Index:      0,
		ID:         "native-cluster",
		Driver:     "native",
		Native:     "deepseek-v4",
		Concurrent: 2,
		Active:     1,
	}

	// 执行释放
	d.Release(staleRes, "native")

	if staleRes.Active != 0 {
		t.Fatalf("expected staleRes.Active == 0, got %d", staleRes.Active)
	}
	// 核心断言：当前生效的 currentRes.Active 必须被精准扣减为 0！
	if currentRes.Active != 0 {
		t.Fatalf("expected currentRes.Active == 0, got %d (stale pointer did not decrement current resource)", currentRes.Active)
	}
}

// TestDispatcher_ResetActiveSlots_Recovery 验证现网槽位被孤儿泄漏占满死锁时，ResetActiveSlots 能否清零并唤醒等待者
func TestDispatcher_ResetActiveSlots_Recovery(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{
			Index:      0,
			ID:         "opencode-deepseek",
			Driver:     "opencode",
			OpenCode:   "deepseek-chat",
			Concurrent: 2,
			Active:     2, // 模拟槽位被孤儿计数全部占满（达到 limit=2）
		},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acquiredChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	// 协程尝试 Acquire，此时应当因槽位已满被阻塞在 cond.Wait
	go func() {
		res, _, err := d.Acquire(ctx, "opencode")
		if err != nil {
			errChan <- err
			return
		}
		if res != nil {
			d.Release(res, "opencode")
			acquiredChan <- true
		}
	}()

	// 等待 50ms 确保协程已经挂起在 cond.Wait
	time.Sleep(50 * time.Millisecond)
	select {
	case <-acquiredChan:
		t.Fatalf("Acquire should have been blocked because active == concurrent")
	default:
		// 阻塞符合预期
	}

	// 执行紧急重置自愈
	cleared := d.ResetActiveSlots()
	if cleared != 2 {
		t.Fatalf("expected cleared == 2, got %d", cleared)
	}
	if d.resources[0].Active != 0 {
		t.Fatalf("expected active to be reset to 0, got %d", d.resources[0].Active)
	}

	// 唤醒后协程应当能够成功获取到槽位
	select {
	case err := <-errChan:
		t.Fatalf("Acquire failed after reset: %v", err)
	case <-acquiredChan:
		// 成功解救唤醒
	case <-time.After(1 * time.Second):
		t.Fatalf("Acquire was not unblocked by ResetActiveSlots")
	}
}
