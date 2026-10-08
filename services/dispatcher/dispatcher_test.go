package dispatcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

type observabilityCaptureInvoker struct {
	req *invoker.AIRequest
}

func (i *observabilityCaptureInvoker) Name() string { return "opencode" }

func (i *observabilityCaptureInvoker) Invoke(req invoker.AIRequest) error {
	if req.Observability == nil {
		return fmt.Errorf("dispatcher did not inject observability")
	}
	i.req = &req
	req.Observability.Console.Append("stdout", "stdout", "wrapper console capture")
	return nil
}

func TestDispatchingInvoker_InjectsObservabilityIntoConsoleRing(t *testing.T) {
	d := setupTestDispatcher(1)
	defer func() {
		close(d.stopHeartbeat)
	}()

	delegate := &observabilityCaptureInvoker{}
	inv := NewDispatchingInvoker(delegate, d)
	if inv == nil {
		t.Fatal("expected dispatching invoker")
	}

	err := inv.Invoke(invoker.AIRequest{
		ParentContext: context.Background(),
		PromptMsg:     "readonly diagnostic test",
		ModelName:     "test-opencode",
		OutputPath:    filepath.Join(t.TempDir(), "out.json"),
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if delegate.req == nil || delegate.req.Observability == nil {
		t.Fatal("delegate did not receive observability")
	}

	captured := delegate.req.Observability
	lease, ok := d.GetLeaseByID(captured.LeaseID)
	if !ok {
		t.Fatalf("lease %s not found in recent history", captured.LeaseID)
	}
	if lease.Observability != captured {
		t.Fatal("lease observability and request observability do not match")
	}

	snapshot, status := lease.Observability.Console.Snapshot(0, 100)
	if status.TotalEvents == 0 {
		t.Fatal("expected console events")
	}
	found := false
	for _, event := range snapshot.Events {
		if event.Stream == "stdout" && event.Message == "wrapper console capture" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected captured console event, got %+v", snapshot.Events)
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

func TestDispatcher_SetWorkHoursThrottleImmediate(t *testing.T) {
	d := setupTestDispatcher(2)
	defer func() {
		close(d.stopHeartbeat)
	}()

	cfg := models.WorkHoursThrottleConfig{
		Enabled:   true,
		Workdays:  []int{3},
		StartTime: "09:00",
		EndTime:   "18:00",
		Scale:     0.25,
	}
	d.SetWorkHoursThrottle(cfg)

	now := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)
	d.mu.Lock()
	info := d.getEffectiveScaleInfoLocked(now)
	d.mu.Unlock()

	if info.ThrottleMode != "work_hours" {
		t.Fatalf("expected mode 'work_hours', got %s", info.ThrottleMode)
	}
	if info.EffectiveScale != 0.25 {
		t.Fatalf("expected scale 0.25, got %f", info.EffectiveScale)
	}
	if !reflect.DeepEqual(info.WorkHoursConfig, cfg) {
		t.Fatalf("expected dispatcher config %+v, got %+v", cfg, info.WorkHoursConfig)
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

	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{{
		ID:     "native",
		Driver: "native",
		Endpoints: []models.ResourceEndpointConfig{{
			Name:       "default",
			BaseURL:    "http://192.168.56.18:8000/v1/chat/completions",
			Model:      "glm-4-flash",
			Concurrent: 20,
		}},
	}}

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

func TestDispatcher_NativeEndpointPoolRegistration(t *testing.T) {
	origConfig := models.AppConfig
	defer func() { models.AppConfig = origConfig }()

	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{{
		ID:     "native",
		Driver: "native",
		Endpoints: []models.ResourceEndpointConfig{
			{Name: "h100", Model: "/GLM-5.3-Flash", Concurrent: 10},
			{Name: "newgate", Model: "deepseek", Concurrent: 10},
		},
	}}

	InitModelDispatcher()
	if GlobalDispatcher == nil || !GlobalDispatcher.enabled {
		t.Fatal("expected GlobalDispatcher to be enabled")
	}
	defer GlobalDispatcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, model, err := GlobalDispatcher.Acquire(ctx, "native")
	if err != nil {
		t.Fatalf("Acquire failed for native endpoint pool: %v", err)
	}
	if res == nil {
		t.Fatal("expected native endpoint pool to be allocated, got nil")
	}
	if model != "native" {
		t.Fatalf("expected model marker 'native', got %q", model)
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

	oldResources := models.AppConfig.LLM.Resources
	models.AppConfig.LLM.Resources = append(oldResources, models.ComputeResourceConfig{
		ID:        mockBackend,
		Driver:    mockBackend,
		Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "custom-tier1-model"}},
	})
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resource: mockBackend,
	}

	tr := &TierRouter{dispatcher: d}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	acq, err := tr.AcquireTier(ctx, "tier1_hunter", "")
	if err != nil {
		t.Fatalf("AcquireTier failed: %v", err)
	}
	defer acq.Release()
	models.AppConfig.LLM.Resources = oldResources

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

func TestDispatchingInvoker_AlignsRequestModelWithAcquiredSlot(t *testing.T) {
	d := &ModelDispatcher{
		manualScale:   1.0,
		stopHeartbeat: make(chan struct{}),
		enabled:       true,
	}
	d.cond = sync.NewCond(&d.mu)
	d.resources = []*ModelResource{
		{Index: 0, ID: "pool-a", OpenCode: "model-a", Concurrent: 1, Active: 1},
		{Index: 1, ID: "pool-b", OpenCode: "model-b", Concurrent: 1},
	}
	GlobalDispatcher = d
	defer func() {
		close(d.stopHeartbeat)
	}()

	mockInv := &mockInvoker{NameStr: "opencode"}

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
		// The logical router chose pool-a, but pool-a became full before the
		// physical slot was acquired. The slot must win over the stale model.
		ModelName: "model-a",
	}

	if err := wrappedInv.Invoke(req); err != nil {
		t.Fatalf("invoker.Invoke failed: %v", err)
	}

	if mockInv.LastModel != "model-b" {
		t.Fatalf("expected backend request to use acquired slot model 'model-b', got '%s'", mockInv.LastModel)
	}
}

func TestTierRouter_MultiResourcePooling(t *testing.T) {
	d := &ModelDispatcher{
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
	d.cond = sync.NewCond(&d.mu)

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

func TestTierResourcePlanSelectsUniqueResourceID(t *testing.T) {
	d := &ModelDispatcher{
		enabled:     true,
		manualScale: 1.0,
		resources: []*ModelResource{
			{Index: 0, ID: "opencode-deepseek", Driver: "opencode", Model: "modelgate/dp", OpenCode: "modelgate/dp", Concurrent: 20, Active: 20},
			{Index: 1, ID: "codex-deepseek", Driver: "codex", Model: "profile:deepseek", Codex: "profile:deepseek", Concurrent: 60},
		},
	}
	d.cond = sync.NewCond(&d.mu)
	models.AppConfig.LLM.Resources = []models.ComputeResourceConfig{
		{ID: "opencode-deepseek", Driver: "opencode", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "modelgate/dp"}}},
		{ID: "codex-deepseek", Driver: "codex", Endpoints: []models.ResourceEndpointConfig{{Name: "default", Model: "profile:deepseek"}}},
	}
	models.AppConfig.Scanner.Debate.Tiers.Tier1Hunter = models.TierBindingConfig{
		Resources: []string{"opencode-deepseek", "codex-deepseek"},
	}

	tr := &TierRouter{dispatcher: d}
	plan, err := tr.AcquireTierResourcePlan(context.Background(), "tier1_hunter", nil)
	if err != nil {
		t.Fatalf("AcquireTierResourcePlan failed: %v", err)
	}
	if plan.Selected.ResourceID != "codex-deepseek" || plan.Selected.Driver != "codex" {
		t.Fatalf("expected codex-deepseek, got id=%s driver=%s", plan.Selected.ResourceID, plan.Selected.Driver)
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
