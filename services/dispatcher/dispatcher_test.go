package dispatcher

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
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
			Native:     "test-native",
			Concurrent: rawConcurrent,
			Active:     0,
		},
	}
	GlobalDispatcher = d
	return d
}

func TestDispatcher_CeilCalculation(t *testing.T) {
	limit := calculateLimit(1, 0.25)
	if limit != 1 {
		t.Fatalf("expected calculateLimit(1, 0.25) == 1, got %d", limit)
	}

	limit0 := calculateLimit(5, 0.0)
	if limit0 != 0 {
		t.Fatalf("expected calculateLimit(5, 0.0) == 0, got %d", limit0)
	}

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

	select {
	case err := <-errChan:
		t.Fatalf("Acquire returned error: %v", err)
	case <-acquiredChan:
		elapsed := time.Since(start)
		if elapsed < 100*time.Millisecond {
			t.Fatalf("Acquired too quickly before expiration (%v)", elapsed)
		}
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

	models.AppConfig.AI.WorkHoursThrottle = models.WorkHoursThrottleConfig{
		Enabled:   true,
		Workdays:  []int{1, 2, 3, 4, 5},
		StartTime: "09:00",
		EndTime:   "22:00",
		Scale:     0.10,
	}
	d.SetWorkHoursThrottle(models.AppConfig.AI.WorkHoursThrottle)

	monday10am := time.Date(2026, 8, 17, 10, 0, 0, 0, time.Local)
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

	saturday := time.Date(2026, 8, 22, 14, 0, 0, 0, time.Local)
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
	d.SetWorkHoursThrottle(models.AppConfig.AI.WorkHoursThrottle)

	infoWork := d.getEffectiveScaleInfoLocked(now)
	if infoWork.ThrottleMode != "work_hours" || infoWork.EffectiveScale != 0.10 {
		t.Fatalf("expected work_hours (0.10), got mode=%s, scale=%f", infoWork.ThrottleMode, infoWork.EffectiveScale)
	}

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
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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

	models.AppConfig.AI.Models = []models.ModelConfig{
		{Claude: "claude-3-5", Concurrent: 3},
	}
	models.AppConfig.AI.Native = models.NativeLLMConfig{
		BaseURL:      "http://192.168.56.18:8000/v1/chat/completions",
		DefaultModel: "glm-4-flash",
	}

	InitModelDispatcher()
	if GlobalDispatcher == nil || !GlobalDispatcher.enabled {
		t.Fatal("expected GlobalDispatcher to be enabled")
	}
	defer GlobalDispatcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, model, err := GlobalDispatcher.Acquire(ctx, "native")
	if err != nil {
		t.Fatalf("Acquire failed for native: %v", err)
	}
	if res == nil {
		t.Fatal("expected native resource to be allocated, got nil")
	}
	if model != "glm-4-flash" {
		t.Fatalf("expected model 'glm-4-flash', got '%s'", model)
	}
	GlobalDispatcher.Release(res, "native")
}

func TestTierRouter_NoDeadlockWithDispatchingInvoker(t *testing.T) {
	origConfig := models.AppConfig
	defer func() { models.AppConfig = origConfig }()

	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	mockBackend := "mock-tier-test"
	mockInv := &mockInvoker{NameStr: mockBackend}
	invoker.RegisterAIInvoker(mockBackend, mockInv)

	models.AppConfig.AI.Tiers.Tier1Fast.Backend = mockBackend
	models.AppConfig.AI.Tiers.Tier1Fast.Model = "custom-tier1-model"

	tr := &TierRouter{dispatcher: d}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acq, err := tr.AcquireTier(ctx, "tier1_fast", "")
	if err != nil {
		t.Fatalf("AcquireTier failed: %v", err)
	}
	defer acq.Release()

	if acq.ModelName != "custom-tier1-model" {
		t.Fatalf("expected ModelName 'custom-tier1-model', got '%s'", acq.ModelName)
	}

	rawInv, ok := invoker.GetRawInvoker(acq.Backend)
	if !ok {
		t.Fatalf("failed to get invoker for %s", acq.Backend)
	}
	wrappedInv := NewDispatchingInvoker(rawInv, d)

	tmpFile, err := os.CreateTemp("", "tier-deadlock-test-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := invoker.AIRequest{
		ParentContext: ctx,
		OutputPath:    tmpPath,
		ModelName:     acq.ModelName,
	}

	if err := wrappedInv.Invoke(req); err != nil {
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
	mockInv := &mockInvoker{NameStr: mockBackend}
	invoker.RegisterAIInvoker(mockBackend, mockInv)

	wrappedInv := NewDispatchingInvoker(mockInv, d)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tmpFile, err := os.CreateTemp("", "preserve-model-test-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := invoker.AIRequest{
		ParentContext: ctx,
		OutputPath:    tmpPath,
		ModelName:     "explicit-model-name",
	}

	if err := wrappedInv.Invoke(req); err != nil {
		t.Fatalf("invoker.Invoke failed: %v", err)
	}

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
				Active:     3,
			},
			{
				Index:      1,
				ID:         "opencode",
				Driver:     "opencode",
				Model:      "models/glm5.1",
				OpenCode:   "models/glm5.1",
				Concurrent: 5,
				Active:     0,
			},
		},
	}

	backend, model := d.PickBestCandidateResource([]string{"agy", "opencode"})
	if backend != "opencode" || model != "models/glm5.1" {
		t.Fatalf("expected opencode to be picked, got backend=%s, model=%s", backend, model)
	}

	d.resources[1].Active = 5
	backend2, model2 := d.PickBestCandidateResource([]string{"agy", "opencode"})
	if backend2 != "agy" || model2 != "gemini-3.7-flash" {
		t.Fatalf("expected agy to be picked when opencode is full, got backend=%s, model=%s", backend2, model2)
	}

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

	workCtx := &invoker.LLMWorkContext{
		ReportID: 167,
		RepoName: "fmt",
		TaskType: "Coredump 风险分析",
		Stage:    "Tier 1: 初筛猎手",
		SubTask:  "分片 1/3 (src/fmt/print.go)",
	}

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

	d.UnregisterSlotLease(leaseID)
	leasesAfter := d.GetActiveLeases()
	if len(leasesAfter) != 0 {
		t.Fatalf("expected 0 active leases after unregister, got %d", len(leasesAfter))
	}

	d.RegisterSlotLease(d.resources[0], "agy", "gemini-3.0-flash-high", workCtx)
	if len(d.GetActiveLeases()) != 1 {
		t.Fatalf("expected 1 active lease before reset")
	}
	d.ResetActiveSlots()
	if len(d.GetActiveLeases()) != 0 {
		t.Fatalf("expected 0 active leases after ResetActiveSlots")
	}
}
