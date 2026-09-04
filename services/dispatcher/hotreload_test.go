package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"code-shield/models"
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
	GlobalDispatcher = d
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

	if len(d.resources) != 1 {
		t.Fatalf("expected 1 reloaded resource, got %d", len(d.resources))
	}
	// 就地复用机制：当前新资源的并发已更新为 10，且活跃计数自动继承为 1
	if d.resources[0].Concurrent != 10 {
		t.Fatalf("expected reloaded concurrent to be 10, got %d", d.resources[0].Concurrent)
	}
	if d.resources[0].Active != 1 {
		t.Fatalf("expected reloaded active count to be preserved as 1, got %d", d.resources[0].Active)
	}

	// 3. 在途调用完成执行并调用 Release
	d.Release(res, "opencode")

	// 4. 验证：当前生效资源池中的对象计数成功归零！
	if d.resources[0].Active != 0 {
		t.Fatalf("expected active count in current resource pool to be 0 after in-flight release, got %d", d.resources[0].Active)
	}
}

// TestDispatcher_HotReload_FullReplacement 模拟极端重载场景（资源被重新全量构建、指针完全分裂）
func TestDispatcher_HotReload_FullReplacement(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	oldRes := &ModelResource{
		Index:      0,
		ID:         "claude-sonnet",
		Driver:     "claude",
		Claude:     "claude-3-5",
		Concurrent: 3,
		Active:     1, // 模拟有一笔飞行中未归还的调用
	}
	d.resources = []*ModelResource{oldRes}
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 模拟极端非就地替换：完全分配了一个新指针对象放入 d.resources（模拟由于 ID/Driver 匹配重构的新实例）
	newRes := &ModelResource{
		Index:      0,
		ID:         "claude-sonnet",
		Driver:     "claude",
		Claude:     "claude-3-5",
		Concurrent: 5,
		Active:     1, // 继承了活跃度
	}
	d.resources = []*ModelResource{newRes}

	// 历史在途调用持有 oldRes 完成并调用 Release
	d.Release(oldRes, "claude")

	// 验证：oldRes 自身 Active 扣减为 0
	if oldRes.Active != 0 {
		t.Fatalf("expected oldRes.Active == 0, got %d", oldRes.Active)
	}
	// 验证：ResourceKey 匹配的新生效对象 newRes 的 Active 也被精准扣减为 0，彻底根除孤儿泄漏！
	if newRes.Active != 0 {
		t.Fatalf("expected newRes.Active == 0 via ResourceKey fallback matching, got %d", newRes.Active)
	}
}

// TestDispatcher_ResetActiveSlots_Recovery 验证极端孤儿泄漏时一键运维自愈接口
func TestDispatcher_ResetActiveSlots_Recovery(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
		activeLeases:  make(map[string]*LLMSlotLease),
	}
	d.cond = sync.NewCond(&d.mu)
	res := &ModelResource{
		Index:      0,
		ID:         "broken-server",
		Driver:     "claude",
		Claude:     "claude-3-5",
		Concurrent: 2,
		Active:     2, // 假死打满
	}
	d.resources = []*ModelResource{res}
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 此时 Acquire 应当超时失败（已打满）
	_, _, err := d.Acquire(ctx, "claude")
	if err == nil {
		t.Fatal("expected Acquire to block and timeout on full resources")
	}

	// 执行运维一键自愈
	cleared := d.ResetActiveSlots()
	if cleared != 2 {
		t.Fatalf("expected 2 slots cleared, got %d", cleared)
	}
	if res.Active != 0 {
		t.Fatalf("expected res.Active == 0 after ResetActiveSlots, got %d", res.Active)
	}

	// 验证自愈后槽位立刻可用
	ctxOk, cancelOk := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelOk()

	resOk, _, errOk := d.Acquire(ctxOk, "claude")
	if errOk != nil {
		t.Fatalf("expected Acquire to succeed after ResetActiveSlots, got err: %v", errOk)
	}
	d.Release(resOk, "claude")
}
