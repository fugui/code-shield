package dispatcher

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"code-shield/models"
)

// TestDispatcher_WeightedProportionalAllocation 验证容量为 30 和 8 的两个池子分配 10 个请求时呈现严格的 8:2 加权比例
func TestDispatcher_WeightedProportionalAllocation(t *testing.T) {
	origThrottle := models.AppConfig.AI.WorkHoursThrottle
	models.AppConfig.AI.WorkHoursThrottle.Enabled = false
	defer func() {
		models.AppConfig.AI.WorkHoursThrottle = origThrottle
	}()

	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{Index: 0, ID: "pool-large", OpenCode: "deepseek-large", Concurrent: 30},
		{Index: 1, ID: "pool-small", OpenCode: "glm-small", Concurrent: 8},
	}
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var acquiredResources []*ModelResource
	allocCount := make(map[string]int)

	for i := 0; i < 10; i++ {
		res, _, err := d.Acquire(ctx, "opencode")
		if err != nil {
			t.Fatalf("Acquire #%d failed: %v", i+1, err)
		}
		if res == nil {
			t.Fatalf("Acquire #%d returned nil resource", i+1)
		}
		acquiredResources = append(acquiredResources, res)
		allocCount[res.ID]++
	}

	largeCount := allocCount["pool-large"]
	smallCount := allocCount["pool-small"]
	t.Logf("Allocated 10 requests: pool-large=%d, pool-small=%d (Large Active=%d/30, Small Active=%d/8)",
		largeCount, smallCount, d.resources[0].Active, d.resources[1].Active)

	if largeCount != 8 || smallCount != 2 {
		t.Fatalf("expected strictly 8 allocated to pool-large and 2 to pool-small, got %d and %d", largeCount, smallCount)
	}

	for _, res := range acquiredResources {
		d.Release(res, "opencode")
	}

	if d.resources[0].Active != 0 || d.resources[1].Active != 0 {
		t.Fatalf("expected all resources to have Active=0 after release, got %d and %d",
			d.resources[0].Active, d.resources[1].Active)
	}
}

// TestDispatcher_WeightedDynamicBalance 模拟不同并发请求并测试动态均衡
func TestDispatcher_WeightedDynamicBalance(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
		workHours:     models.WorkHoursThrottleConfig{Enabled: false},
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{Index: 0, ID: "server-60", OpenCode: "model-a", Concurrent: 60},
		{Index: 1, ID: "server-20", OpenCode: "model-b", Concurrent: 20},
	}
	GlobalDispatcher = d
	defer d.Close()

	var wg sync.WaitGroup
	ctx := context.Background()

	allocMap := make(map[string]int)
	var mapMu sync.Mutex

	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)

			res, _, err := d.Acquire(ctx, "opencode")
			if err != nil {
				t.Errorf("Acquire failed: %v", err)
				return
			}
			mapMu.Lock()
			allocMap[res.ID]++
			mapMu.Unlock()

			time.Sleep(20 * time.Millisecond)
			d.Release(res, "opencode")
		}()
	}

	wg.Wait()

	t.Logf("Dynamic load results: server-60: %d, server-20: %d", allocMap["server-60"], allocMap["server-20"])
	if allocMap["server-60"] <= allocMap["server-20"] {
		t.Errorf("expected server-60 to handle significantly more requests than server-20, got %d vs %d",
			allocMap["server-60"], allocMap["server-20"])
	}
}
