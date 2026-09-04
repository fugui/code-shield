package services

import (
	"code-shield/models"
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

func setupTestDispatcher(rawConcurrent int) *ModelDispatcher {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{
			Index:      0,
			OpenCode:   "test-opencode",
			Claude:     "test-claude",
			Codex:      "test-codex",
			Agy:        "test-agy",
			Concurrent: rawConcurrent,
			Active:     0,
		},
	}
	Dispatcher = d
	return d
}

func TestDispatcher_CeilCalculation(t *testing.T) {
	// 当 rawConcurrent = 1, scale = 0.25 时，limit 应当向上取整为 1
	limit := calculateLimit(1, 0.25)
	if limit != 1 {
		t.Fatalf("expected calculateLimit(1, 0.25) == 1, got %d", limit)
	}

	// 当 scale = 0 时，limit 应当为 0（暂停）
	limit0 := calculateLimit(5, 0.0)
	if limit0 != 0 {
		t.Fatalf("expected calculateLimit(5, 0.0) == 0, got %d", limit0)
	}

	// 当 rawConcurrent = 2, scale = 0.5 时，limit 为 1
	limitHalf := calculateLimit(2, 0.5)
	if limitHalf != 1 {
		t.Fatalf("expected calculateLimit(2, 0.5) == 1, got %d", limitHalf)
	}
}

func TestDispatcher_AutoWakeupOnExpiration(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 1. 设置限速为 0%（完全暂停），持续 150 毫秒
	duration := 150 * time.Millisecond
	d.SetScale(0.0, duration)

	scale, expiresAt := d.GetScaleAndExpiration()
	if scale != 0.0 {
		t.Fatalf("expected scale 0.0, got %f", scale)
	}
	if expiresAt.IsZero() {
		t.Fatalf("expected non-zero expiration time")
	}

	acquiredChan := make(chan bool, 1)
	errChan := make(chan error, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 2. 启动协程尝试 Acquire，此时应当被阻塞
	start := time.Now()
	go func() {
		res, model, err := d.Acquire(ctx, "claude")
		if err != nil {
			errChan <- err
			return
		}
		if res != nil && model == "test-claude" {
			d.Release(res, "claude")
			acquiredChan <- true
		}
	}()

	// 3. 等待获取结果
	select {
	case err := <-errChan:
		t.Fatalf("Acquire returned error: %v", err)
	case <-acquiredChan:
		elapsed := time.Since(start)
		if elapsed < 100*time.Millisecond {
			t.Fatalf("Acquired too quickly before expiration (%v)", elapsed)
		}
		// 校验过期后 scale 是否已恢复为 1.0
		scaleAfter, _ := d.GetScaleAndExpiration()
		if scaleAfter != 1.0 {
			t.Fatalf("expected scale to be restored to 1.0, got %f", scaleAfter)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Acquire timed out! Goroutine was not woken up after throttle expiration.")
	}
}

func TestDispatcher_ContextCancellation(t *testing.T) {
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 设置 scale = 0 永久暂停
	d.SetScale(0.0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := d.Acquire(ctx, "claude")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Acquire did not exit promptly on context cancellation, took %v", elapsed)
	}
}

func TestDispatcher_ScaleClamp(t *testing.T) {
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	d.SetScale(-1.0, 0)
	s, _ := d.GetScaleAndExpiration()
	if s != 0.0 {
		t.Fatalf("expected negative scale to be clamped to 0.0, got %f", s)
	}

	d.SetScale(5.0, 0)
	s, _ = d.GetScaleAndExpiration()
	if s != 3.0 {
		t.Fatalf("expected scale > 3.0 to be clamped to 3.0, got %f", s)
	}
}

func TestDispatcher_WorkHoursThrottle(t *testing.T) {
	d := setupTestDispatcher(10)
	defer func() {
		close(d.stopHeartbeat)
	}()

	// 模拟配置：周一到周五 09:00 ~ 22:00 自动限速至 10% (0.10)
	models.AppConfig.AI.WorkHoursThrottle = models.WorkHoursThrottleConfig{
		Enabled:   true,
		Workdays:  []int{1, 2, 3, 4, 5},
		StartTime: "09:00",
		EndTime:   "22:00",
		Scale:     0.10,
	}

	// 1. 模拟周一上午 10:00（应当命中工作时间）
	monday10am := time.Date(2026, 8, 17, 10, 0, 0, 0, time.Local) // 2026-08-17 is Monday
	infoWork := d.getEffectiveScaleInfoLocked(monday10am)
	if infoWork.ThrottleMode != "work_hours" {
		t.Fatalf("expected mode 'work_hours', got %s", infoWork.ThrottleMode)
	}
	if infoWork.EffectiveScale != 0.10 {
		t.Fatalf("expected effective scale 0.10, got %f", infoWork.EffectiveScale)
	}
	if !infoWork.IsWorkHours {
		t.Fatalf("expected IsWorkHours == true")
	}

	// 2. 模拟周一晚上 23:00（非工作时间，应当恢复 1.0）
	monday11pm := time.Date(2026, 8, 17, 23, 0, 0, 0, time.Local)
	infoOff := d.getEffectiveScaleInfoLocked(monday11pm)
	if infoOff.ThrottleMode != "normal" {
		t.Fatalf("expected mode 'normal', got %s", infoOff.ThrottleMode)
	}
	if infoOff.EffectiveScale != 1.0 {
		t.Fatalf("expected effective scale 1.0, got %f", infoOff.EffectiveScale)
	}
	if infoOff.IsWorkHours {
		t.Fatalf("expected IsWorkHours == false")
	}

	// 3. 模拟周六（周末非工作日，应当为 1.0）
	saturday := time.Date(2026, 8, 22, 14, 0, 0, 0, time.Local) // 2026-08-22 is Saturday
	infoWeekend := d.getEffectiveScaleInfoLocked(saturday)
	if infoWeekend.ThrottleMode != "normal" || infoWeekend.EffectiveScale != 1.0 {
		t.Fatalf("expected weekend to be normal mode with 1.0, got mode=%s, scale=%f",
			infoWeekend.ThrottleMode, infoWeekend.EffectiveScale)
	}
}

func TestDispatcher_ManualOverrideWorkHours(t *testing.T) {
	d := setupTestDispatcher(10)
	defer func() {
		close(d.stopHeartbeat)
	}()

	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	startHM := now.Add(-1 * time.Hour).Format("15:04")
	endHM := now.Add(2 * time.Hour).Format("15:04")

	models.AppConfig.AI.WorkHoursThrottle = models.WorkHoursThrottleConfig{
		Enabled:   true,
		Workdays:  []int{weekday},
		StartTime: startHM,
		EndTime:   endHM,
		Scale:     0.10,
	}

	// 1. 当前时刻无手动覆盖时，应当命中工作时间限速 0.10
	infoWork := d.getEffectiveScaleInfoLocked(now)
	if infoWork.ThrottleMode != "work_hours" || infoWork.EffectiveScale != 0.10 {
		t.Fatalf("expected work_hours (0.10), got mode=%s, scale=%f", infoWork.ThrottleMode, infoWork.EffectiveScale)
	}

	// 2. 管理员手动调节为 50% (0.50)，持续 1 小时
	d.SetScale(0.50, 1*time.Hour)

	infoManual := d.getEffectiveScaleInfoLocked(now)
	if infoManual.ThrottleMode != "manual" {
		t.Fatalf("expected manual override to take precedence, got mode %s", infoManual.ThrottleMode)
	}
	if infoManual.EffectiveScale != 0.50 {
		t.Fatalf("expected manual scale 0.50, got %f", infoManual.EffectiveScale)
	}
	if !infoManual.IsManual {
		t.Fatalf("expected IsManual == true")
	}

	// 3. 手动重置后，应当自动回退到工作时间的 0.10
	d.RestoreManualScale()
	infoAfterReset := d.getEffectiveScaleInfoLocked(now)
	if infoAfterReset.ThrottleMode != "work_hours" || infoAfterReset.EffectiveScale != 0.10 {
		t.Fatalf("expected rollback to work_hours (0.10), got mode=%s, scale=%f",
			infoAfterReset.ThrottleMode, infoAfterReset.EffectiveScale)
	}
}

func TestDispatcher_CodexRouting(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, model, err := d.Acquire(ctx, "codex")
	if err != nil {
		t.Fatalf("Acquire failed for codex: %v", err)
	}
	if res == nil || model != "test-codex" {
		t.Fatalf("expected model 'test-codex', got '%s'", model)
	}
	d.Release(res, "codex")
}

func TestDispatcher_LeastLoadedSelection(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{Index: 0, Claude: "server-a", Concurrent: 1},
		{Index: 1, Claude: "server-b", Concurrent: 1},
	}
	Dispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 两台服务器均空闲时优先第一台，第二台应落在另一台上（而非继续压在第一台）
	res1, model1, err := d.Acquire(ctx, "claude")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	res2, model2, err := d.Acquire(ctx, "claude")
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	if res1.Index == res2.Index {
		t.Fatalf("expected least-loaded balancing across servers, got same server #%d twice", res1.Index)
	}
	if model1 == "" || model2 == "" {
		t.Fatalf("expected non-empty model names, got %q / %q", model1, model2)
	}
	d.Release(res1, "claude")
	d.Release(res2, "claude")
}

func TestDispatcher_AgyRouting(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, model, err := d.Acquire(ctx, "agy")
	if err != nil {
		t.Fatalf("Acquire failed for agy: %v", err)
	}
	if res == nil || model != "test-agy" {
		t.Fatalf("expected model 'test-agy', got '%s'", model)
	}
	d.Release(res, "agy")
}

func TestDispatcher_NativeAutoRegistration(t *testing.T) {
	origConfig := models.AppConfig
	defer func() { models.AppConfig = origConfig }()

	// 配置只包含 CLI 模型的 models 列表，但开启了 ai.native
	models.AppConfig.AI.Models = []models.ModelConfig{
		{Claude: "claude-3-5", Concurrent: 3},
	}
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:      "http://192.168.56.18:8000/v1/chat/completions",
		DefaultModel: "glm-4-flash",
	}

	InitModelDispatcher()
	if Dispatcher == nil || !Dispatcher.enabled {
		t.Fatal("expected Dispatcher to be enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 验证请求 native 不会直通告警，而是正常分配 native 槽位
	res, model, err := Dispatcher.Acquire(ctx, "native")
	if err != nil {
		t.Fatalf("Acquire failed for native: %v", err)
	}
	if res == nil {
		t.Fatal("expected native resource to be allocated, got nil")
	}
	if model != "glm-4-flash" {
		t.Fatalf("expected model 'glm-4-flash', got '%s'", model)
	}
	Dispatcher.Release(res, "native")
}

func TestTierRouter_NoDeadlockWithDispatchingInvoker(t *testing.T) {
	origConfig := models.AppConfig
	defer func() { models.AppConfig = origConfig }()

	// 1. 设置仅有 1 个并发槽位的调度器 (模拟 scale 缩容或单槽位极限情况)
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	mockBackend := "mock-tier-test"
	mockInv := &MockInvoker{NameStr: mockBackend}
	RegisterAIInvoker(mockBackend, mockInv)

	models.AppConfig.AI.Tiers.Tier1Fast.Backend = mockBackend
	models.AppConfig.AI.Tiers.Tier1Fast.Model = "custom-tier1-model"

	tr := &TierRouter{dispatcher: d}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 2. 模拟 runHunterStage: 先调 AcquireTier，再调 callAITier (GetAIInvoker -> DispatchingInvoker)
	acq, err := tr.AcquireTier(ctx, "tier1_fast", "")
	if err != nil {
		t.Fatalf("AcquireTier failed: %v", err)
	}
	defer acq.Release()

	if acq.ModelName != "custom-tier1-model" {
		t.Fatalf("expected ModelName 'custom-tier1-model', got '%s'", acq.ModelName)
	}

	invoker := GetAIInvoker(acq.Backend)
	tmpFile, err := os.CreateTemp("", "tier-deadlock-test-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := AIRequest{
		ParentContext: ctx,
		OutputPath:    tmpPath,
		ModelName:     acq.ModelName,
	}

	// 3. 执行 Invoke，若存在双重加锁自锁，此处必然超时阻塞
	if err := invoker.Invoke(req); err != nil {
		t.Fatalf("invoker.Invoke failed (possible deadlock): %v", err)
	}

	if mockInv.InvokedCnt != 1 {
		t.Fatalf("expected mock invoker to be invoked once, got %d", mockInv.InvokedCnt)
	}
}

func TestDispatchingInvoker_PreserveExplicitModelName(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	mockBackend := "mock-preserve-model"
	mockInv := &MockInvoker{NameStr: mockBackend}
	RegisterAIInvoker(mockBackend, mockInv)

	invoker := GetAIInvoker(mockBackend)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tmpFile, err := os.CreateTemp("", "preserve-model-test-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := AIRequest{
		ParentContext: ctx,
		OutputPath:    tmpPath,
		ModelName:     "explicit-model-name",
	}

	if err := invoker.Invoke(req); err != nil {
		t.Fatalf("invoker.Invoke failed: %v", err)
	}

	// 验证请求中显式设置的 ModelName 没有被冲掉
	if req.ModelName != "explicit-model-name" {
		t.Fatalf("expected ModelName to be preserved as 'explicit-model-name', got '%s'", req.ModelName)
	}
}

func TestTierRouter_MultiResourcePooling(t *testing.T) {
	d := &ModelDispatcher{
		cond:        sync.NewCond(&sync.Mutex{}),
		enabled:     true,
		manualScale: 1.0,
		resources: []*ModelResource{
			{
				Index:      0,
				ID:         "agy",
				Driver:     "agy",
				Model:      "gemini-3.7-flash",
				Agy:        "gemini-3.7-flash",
				Concurrent: 5,
				Active:     3, // 负载较高
			},
			{
				Index:      1,
				ID:         "opencode",
				Driver:     "opencode",
				Model:      "models/glm5.1",
				OpenCode:   "models/glm5.1",
				Concurrent: 5,
				Active:     0, // 完全空闲
			},
		},
	}

	// 1. 测试 PickBestCandidateResource: 应优先选出空闲的 opencode
	backend, model := d.PickBestCandidateResource([]string{"agy", "opencode"})
	if backend != "opencode" || model != "models/glm5.1" {
		t.Fatalf("expected opencode to be picked, got backend=%s, model=%s", backend, model)
	}

	// 2. 当 opencode 打满 (Active=5)，应自动切换/故障转移至 agy
	d.resources[1].Active = 5
	backend2, model2 := d.PickBestCandidateResource([]string{"agy", "opencode"})
	if backend2 != "agy" || model2 != "gemini-3.7-flash" {
		t.Fatalf("expected agy to be picked when opencode is full, got backend=%s, model=%s", backend2, model2)
	}

	// 3. 测试通过 TierRouter 绑定多资源池调度
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resources:      []string{"agy", "opencode"},
		TimeoutSeconds: 1200,
	}
	tr := &TierRouter{dispatcher: d}
	acq, err := tr.AcquireTier(context.Background(), "tier1_hunter", "")
	if err != nil {
		t.Fatalf("AcquireTier failed: %v", err)
	}
	if acq.Backend != "agy" {
		t.Fatalf("expected TierRouter to select agy, got %s", acq.Backend)
	}
}

func TestModelDispatcher_SlotLeaseLifecycle(t *testing.T) {
	d := &ModelDispatcher{
		cond:         sync.NewCond(&sync.Mutex{}),
		enabled:      true,
		activeLeases: make(map[string]*LLMSlotLease),
		resources: []*ModelResource{
			{
				Index:      0,
				ID:         "agy",
				Driver:     "agy",
				Model:      "gemini-3.0-flash-high",
				Agy:        "gemini-3.0-flash-high",
				Concurrent: 5,
				Active:     2,
			},
		},
	}

	workCtx := &LLMWorkContext{
		ReportID: 167,
		RepoName: "fmt",
		TaskType: "Coredump 风险分析",
		Stage:    "Tier 1: 初筛猎手",
		SubTask:  "分片 1/3 (src/fmt/print.go)",
	}

	// 1. 登记租约
	leaseID := d.RegisterSlotLease(d.resources[0], "agy", "gemini-3.0-flash-high", workCtx)
	if leaseID == "" {
		t.Fatal("expected non-empty leaseID")
	}

	leases := d.GetActiveLeases()
	if len(leases) != 1 {
		t.Fatalf("expected 1 active lease, got %d", len(leases))
	}
	if leases[0].ReportID != 167 || leases[0].RepoName != "fmt" || leases[0].Stage != "Tier 1: 初筛猎手" {
		t.Fatalf("unexpected lease data: %+v", leases[0])
	}

	// 2. 注销租约
	d.UnregisterSlotLease(leaseID)
	leasesAfter := d.GetActiveLeases()
	if len(leasesAfter) != 0 {
		t.Fatalf("expected 0 active leases after unregister, got %d", len(leasesAfter))
	}

	// 3. 测试 ResetActiveSlots 清除租约
	d.RegisterSlotLease(d.resources[0], "agy", "gemini-3.0-flash-high", workCtx)
	if len(d.GetActiveLeases()) != 1 {
		t.Fatalf("expected 1 active lease before reset")
	}
	d.ResetActiveSlots()
	if len(d.GetActiveLeases()) != 0 {
		t.Fatalf("expected 0 active leases after ResetActiveSlots")
	}
}
