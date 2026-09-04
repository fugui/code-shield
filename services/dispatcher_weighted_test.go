package services

import (
	"code-shield/models"
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"
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
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var acquiredResources []*ModelResource
	allocCount := make(map[string]int)

	// 连续申请 10 个槽位
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

	// 验证最终分配：大池子 8 个，小池子 2 个
	largeCount := allocCount["pool-large"]
	smallCount := allocCount["pool-small"]
	t.Logf("Allocated 10 requests: pool-large=%d, pool-small=%d (Large Active=%d/30, Small Active=%d/8)",
		largeCount, smallCount, d.resources[0].Active, d.resources[1].Active)

	if largeCount != 8 || smallCount != 2 {
		t.Fatalf("expected 8:2 proportional allocation, got large=%d, small=%d", largeCount, smallCount)
	}

	// 释放所有槽位，并验证活跃计数器精准归零
	for _, res := range acquiredResources {
		d.Release(res, "opencode")
	}

	if d.resources[0].Active != 0 || d.resources[1].Active != 0 {
		t.Fatalf("expected all active slots to be 0, got large=%d, small=%d",
			d.resources[0].Active, d.resources[1].Active)
	}
}

// TestDispatcher_SWRR_SmoothCandidateDistribution 验证上层 PickBestCandidateResource 平滑交错打散
func TestDispatcher_SWRR_SmoothCandidateDistribution(t *testing.T) {
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
		{Index: 0, ID: "pool-a", Driver: "opencode", Model: "model-a", Concurrent: 30},
		{Index: 1, ID: "pool-b", Driver: "native", Model: "model-b", Concurrent: 8},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	candidateIDs := []string{"pool-a", "pool-b"}
	modelCounts := make(map[string]int)
	var sequence []string

	for i := 0; i < 10; i++ {
		backend, model := d.PickBestCandidateResource(candidateIDs)
		if backend == "" || model == "" {
			t.Fatalf("PickBestCandidateResource #%d returned empty", i+1)
		}
		modelCounts[model]++
		sequence = append(sequence, model)
	}

	t.Logf("SWRR 10-step Sequence: %v", sequence)
	t.Logf("SWRR Counts: model-a=%d, model-b=%d", modelCounts["model-a"], modelCounts["model-b"])

	if modelCounts["model-a"] != 8 || modelCounts["model-b"] != 2 {
		t.Fatalf("expected SWRR to distribute 8:2, got a=%d, b=%d", modelCounts["model-a"], modelCounts["model-b"])
	}

	// 验证分布是平滑交错的（非前 8 个全 A 后 2 个全 B）
	if sequence[0] == sequence[1] && sequence[1] == sequence[2] && sequence[2] == sequence[3] && sequence[3] == sequence[4] &&
		sequence[4] == sequence[5] && sequence[5] == sequence[6] && sequence[6] == sequence[7] {
		t.Fatalf("SWRR should interleave picks smoothly rather than clustering 8 consecutive times")
	}
}

// TestDispatcher_HighConcurrencyDeadlockFree 模拟 50 个高并发 Goroutine 频繁抢占与释放槽位，确保绝对零死锁
func TestDispatcher_HighConcurrencyDeadlockFree(t *testing.T) {
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
		{Index: 0, ID: "server-1", OpenCode: "model-1", Concurrent: 5},
		{Index: 1, ID: "server-2", OpenCode: "model-2", Concurrent: 3},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	const totalWorkers = 50
	var wg sync.WaitGroup
	wg.Add(totalWorkers)

	successCount := 0
	var mu sync.Mutex

	for i := 0; i < totalWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			// 随机带超时上下文，验证取消与排队共存时的抗死锁性
			timeout := 2 * time.Second
			if workerID%5 == 0 {
				timeout = 50 * time.Millisecond // 模拟部分超时取消
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			res, _, err := d.Acquire(ctx, "opencode")
			if err != nil {
				// 超时取消属于正常行为
				return
			}
			if res == nil {
				return
			}

			// 模拟随机处理时长
			sleepMs := rand.Intn(10) + 2
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)

			d.Release(res, "opencode")

			mu.Lock()
			successCount++
			mu.Unlock()
		}(i)
	}

	// 等待所有协程完成（设置硬超时看门狗，若发生死锁测试立即失败）
	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		t.Logf("High concurrency test completed successfully! Total succeeded workers: %d/%d", successCount, totalWorkers)
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK DETECTED! High concurrency workers did not finish within 5s timeout")
	}

	// 最终校验活跃槽位必须全部清零
	if d.resources[0].Active != 0 || d.resources[1].Active != 0 {
		t.Fatalf("Slot leak detected! server-1 active=%d, server-2 active=%d",
			d.resources[0].Active, d.resources[1].Active)
	}
}

// TestDispatcher_SmallPoolSaturationOverflow 验证小池打满后自动平滑溢流至大池
func TestDispatcher_SmallPoolSaturationOverflow(t *testing.T) {
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
		{Index: 0, ID: "large-pool", Claude: "claude-large", Concurrent: 10},
		{Index: 1, ID: "small-pool", Claude: "claude-small", Concurrent: 2},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var held []*ModelResource
	for i := 0; i < 10; i++ {
		res, _, err := d.Acquire(ctx, "claude")
		if err != nil {
			t.Fatalf("Acquire #%d failed: %v", i+1, err)
		}
		held = append(held, res)
	}

	// 小池容量 2 满载，其余 8 个全部溢流到大池
	t.Logf("Allocation result: large=%d/10, small=%d/2", d.resources[0].Active, d.resources[1].Active)
	if d.resources[1].Active != 2 {
		t.Fatalf("expected small pool to be fully saturated at 2, got %d", d.resources[1].Active)
	}
	if d.resources[0].Active != 8 {
		t.Fatalf("expected large pool to absorb overflow and reach 8, got %d", d.resources[0].Active)
	}

	for _, r := range held {
		d.Release(r, "claude")
	}

	if d.resources[0].Active != 0 || d.resources[1].Active != 0 {
		t.Fatalf("Slots not cleared cleanly: large=%d, small=%d", d.resources[0].Active, d.resources[1].Active)
	}
}
